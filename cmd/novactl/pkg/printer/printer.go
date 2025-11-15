// Package printer provides formatted output for novactl commands.
package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// OutputFormat represents the output format type
type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
	OutputYAML  OutputFormat = "yaml"
	OutputWide  OutputFormat = "wide"
)

// Printer handles formatted output
type Printer struct {
	Format OutputFormat
	Writer io.Writer
}

// NewPrinter creates a new printer
func NewPrinter(format OutputFormat, writer io.Writer) *Printer {
	return &Printer{
		Format: format,
		Writer: writer,
	}
}

// PrintResourceList prints a list of resources
func (p *Printer) PrintResourceList(resourceType string, items []unstructured.Unstructured) error {
	if len(items) == 0 {
		fmt.Fprintf(p.Writer, "No %s found.\n", resourceType)
		return nil
	}

	switch p.Format {
	case OutputJSON:
		return p.printJSON(items)
	case OutputYAML:
		return p.printYAML(items)
	case OutputWide, OutputTable:
		return p.printTable(resourceType, items)
	default:
		return fmt.Errorf("unsupported output format: %s", p.Format)
	}
}

// PrintResource prints a single resource
func (p *Printer) PrintResource(item unstructured.Unstructured) error {
	switch p.Format {
	case OutputJSON:
		return p.printJSON([]unstructured.Unstructured{item})
	case OutputYAML:
		return p.printYAML([]unstructured.Unstructured{item})
	default:
		return p.printYAML([]unstructured.Unstructured{item})
	}
}

func (p *Printer) printJSON(items []unstructured.Unstructured) error {
	encoder := json.NewEncoder(p.Writer)
	encoder.SetIndent("", "  ")
	for _, item := range items {
		if err := encoder.Encode(item.Object); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
	}
	return nil
}

func (p *Printer) printYAML(items []unstructured.Unstructured) error {
	for i, item := range items {
		if i > 0 {
			fmt.Fprintln(p.Writer, "---")
		}
		data, err := yaml.Marshal(item.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Fprint(p.Writer, string(data))
	}
	return nil
}

func (p *Printer) printTable(resourceType string, items []unstructured.Unstructured) error {
	w := tabwriter.NewWriter(p.Writer, 0, 8, 2, ' ', 0)
	defer w.Flush()

	// Print header based on resource type
	switch strings.ToLower(resourceType) {
	case "gateways", "proxygateways":
		p.printGatewayTable(w, items)
	case "routes", "proxyroutes":
		p.printRouteTable(w, items)
	case "backends", "proxybackends":
		p.printBackendTable(w, items)
	case "policies", "proxypolicies":
		p.printPolicyTable(w, items)
	case "vips", "proxyvips":
		p.printVIPTable(w, items)
	default:
		p.printGenericTable(w, items)
	}

	return nil
}

func (p *Printer) printGatewayTable(w *tabwriter.Writer, items []unstructured.Unstructured) {
	fmt.Fprintln(w, "NAME\tLISTENERS\tAGE")
	for _, item := range items {
		name := item.GetName()
		age := formatAge(item.GetCreationTimestamp().Time)

		spec, _, _ := unstructured.NestedMap(item.Object, "spec")
		listeners, _, _ := unstructured.NestedSlice(spec, "listeners")
		listenerCount := len(listeners)

		fmt.Fprintf(w, "%s\t%d\t%s\n", name, listenerCount, age)
	}
}

func (p *Printer) printRouteTable(w *tabwriter.Writer, items []unstructured.Unstructured) {
	fmt.Fprintln(w, "NAME\tHOSTNAMES\tBACKENDS\tAGE")
	for _, item := range items {
		name := item.GetName()
		age := formatAge(item.GetCreationTimestamp().Time)

		spec, _, _ := unstructured.NestedMap(item.Object, "spec")
		hostnames, _, _ := unstructured.NestedStringSlice(spec, "hostnames")
		hostnameStr := strings.Join(hostnames, ",")
		if hostnameStr == "" {
			hostnameStr = "*"
		}

		backends, _, _ := unstructured.NestedSlice(spec, "backends")
		backendCount := len(backends)

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", name, hostnameStr, backendCount, age)
	}
}

func (p *Printer) printBackendTable(w *tabwriter.Writer, items []unstructured.Unstructured) {
	fmt.Fprintln(w, "NAME\tENDPOINTS\tHEALTHY\tAGE")
	for _, item := range items {
		name := item.GetName()
		age := formatAge(item.GetCreationTimestamp().Time)

		status, _, _ := unstructured.NestedMap(item.Object, "status")
		endpoints, _, _ := unstructured.NestedSlice(status, "endpoints")
		endpointCount := len(endpoints)

		// Count healthy endpoints
		healthyCount := 0
		for _, ep := range endpoints {
			epMap, ok := ep.(map[string]interface{})
			if !ok {
				continue
			}
			healthy, _, _ := unstructured.NestedBool(epMap, "healthy")
			if healthy {
				healthyCount++
			}
		}

		fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", name, endpointCount, healthyCount, age)
	}
}

func (p *Printer) printPolicyTable(w *tabwriter.Writer, items []unstructured.Unstructured) {
	fmt.Fprintln(w, "NAME\tTYPE\tTARGET\tAGE")
	for _, item := range items {
		name := item.GetName()
		age := formatAge(item.GetCreationTimestamp().Time)

		spec, _, _ := unstructured.NestedMap(item.Object, "spec")

		// Determine policy type
		policyType := "unknown"
		if _, found, _ := unstructured.NestedMap(spec, "rateLimit"); found {
			policyType = "rateLimit"
		} else if _, found, _ := unstructured.NestedMap(spec, "cors"); found {
			policyType = "cors"
		} else if _, found, _ := unstructured.NestedMap(spec, "tlsPolicy"); found {
			policyType = "tls"
		}

		targetRef, _, _ := unstructured.NestedMap(spec, "targetRef")
		targetKind, _, _ := unstructured.NestedString(targetRef, "kind")
		targetName, _, _ := unstructured.NestedString(targetRef, "name")
		target := fmt.Sprintf("%s/%s", targetKind, targetName)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, policyType, target, age)
	}
}

func (p *Printer) printVIPTable(w *tabwriter.Writer, items []unstructured.Unstructured) {
	fmt.Fprintln(w, "NAME\tIP\tMODE\tNODE\tSTATUS\tAGE")
	for _, item := range items {
		name := item.GetName()
		age := formatAge(item.GetCreationTimestamp().Time)

		spec, _, _ := unstructured.NestedMap(item.Object, "spec")
		ip, _, _ := unstructured.NestedString(spec, "ip")
		mode, _, _ := unstructured.NestedString(spec, "mode")

		status, _, _ := unstructured.NestedMap(item.Object, "status")
		assignedNode, _, _ := unstructured.NestedString(status, "assignedNode")
		vipStatus, _, _ := unstructured.NestedString(status, "status")

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", name, ip, mode, assignedNode, vipStatus, age)
	}
}

func (p *Printer) printGenericTable(w *tabwriter.Writer, items []unstructured.Unstructured) {
	fmt.Fprintln(w, "NAME\tKIND\tAGE")
	for _, item := range items {
		name := item.GetName()
		kind := item.GetKind()
		age := formatAge(item.GetCreationTimestamp().Time)
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, kind, age)
	}
}

// formatAge formats a time into a human-readable age string
func formatAge(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}
