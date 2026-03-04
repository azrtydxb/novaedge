package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Gateway API resource GVRs
var (
	gatewayClassGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gatewayclasses",
	}
	gatewayGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gateways",
	}
	httpRouteGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
)

func newGatewayAPICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gateway-api",
		Aliases: []string{"gwapi"},
		Short:   "Manage Gateway API resources",
		Long: `Interact with Gateway API resources (GatewayClass, Gateway, HTTPRoute).
These commands provide a convenient way to inspect Gateway API resources
that are managed by the NovaEdge controller.`,
	}

	cmd.AddCommand(newGetGatewayClassesCommand())
	cmd.AddCommand(newGetGatewayAPIGatewaysCommand())
	cmd.AddCommand(newGetHTTPRoutesCommand())
	cmd.AddCommand(newDescribeGatewayAPIGatewayCommand())

	return cmd
}

func newGetGatewayClassesCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "gatewayclasses",
		Aliases: []string{"gc", "gwc"},
		Short:   "List GatewayClasses",
		Long:    "List all GatewayClass resources in the cluster.",
		RunE:    runGetGatewayClasses,
	}
}

func newGetGatewayAPIGatewaysCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "gateways",
		Aliases: []string{"gw"},
		Short:   "List Gateway API Gateways",
		Long:    "List all Gateway API Gateway resources (not ProxyGateways).",
		RunE:    runGetGatewayAPIGateways,
	}
}

func newGetHTTPRoutesCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "httproutes",
		Aliases: []string{"hr"},
		Short:   "List HTTPRoutes",
		Long:    "List all Gateway API HTTPRoute resources.",
		RunE:    runGetHTTPRoutes,
	}
}

func newDescribeGatewayAPIGatewayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "describe-gateway [name]",
		Short: "Describe a Gateway API Gateway",
		Long:  "Show detailed information about a Gateway API Gateway including status conditions.",
		Args:  cobra.ExactArgs(1),
		RunE:  runDescribeGatewayAPIGateway,
	}
}

func runGetGatewayClasses(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	list, err := dynamicClient.Resource(gatewayClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list GatewayClasses: %w", err)
	}

	if len(list.Items) == 0 {
		fmt.Println("No GatewayClasses found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer func() { _ = w.Flush() }()

	_, _ = fmt.Fprintln(w, "NAME\tCONTROLLER\tACCEPTED\tAGE")

	for _, item := range list.Items {
		name := item.GetName()

		controllerName := ""
		if spec, ok := item.Object["spec"].(map[string]interface{}); ok {
			if cn, ok := spec["controllerName"].(string); ok {
				controllerName = cn
			}
		}

		accepted := statusUnknown
		if status, ok := item.Object["status"].(map[string]interface{}); ok {
			if conditions, ok := status["conditions"].([]interface{}); ok {
				for _, c := range conditions {
					if cond, ok := c.(map[string]interface{}); ok {
						condType, _ := cond["type"].(string)
						condStatus, _ := cond["status"].(string)
						if condType == conditionAccepted {
							if condStatus == conditionTrue {
								accepted = statusYes
							} else {
								accepted = statusNo
							}
							break
						}
					}
				}
			}
		}

		age := formatResourceAge(item.GetCreationTimestamp().Time)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, controllerName, accepted, age)
	}

	return nil
}

func runGetGatewayAPIGateways(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	list, err := dynamicClient.Resource(gatewayGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Gateways: %w", err)
	}

	if len(list.Items) == 0 {
		fmt.Println("No Gateway API Gateways found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer func() { _ = w.Flush() }()

	_, _ = fmt.Fprintln(w, "NAME\tCLASS\tLISTENERS\tACCEPTED\tPROGRAMMED\tAGE")

	for _, item := range list.Items {
		name := item.GetName()

		className := ""
		listenerCount := 0
		if spec, ok := item.Object["spec"].(map[string]interface{}); ok {
			if cn, ok := spec["gatewayClassName"].(string); ok {
				className = cn
			}
			if listeners, ok := spec["listeners"].([]interface{}); ok {
				listenerCount = len(listeners)
			}
		}

		accepted := statusUnknown
		programmed := statusUnknown
		if status, ok := item.Object["status"].(map[string]interface{}); ok {
			if conditions, ok := status["conditions"].([]interface{}); ok {
				for _, c := range conditions {
					if cond, ok := c.(map[string]interface{}); ok {
						condType, _ := cond["type"].(string)
						condStatus, _ := cond["status"].(string)
						switch condType {
						case conditionAccepted:
							if condStatus == conditionTrue {
								accepted = statusYes
							} else {
								accepted = statusNo
							}
						case "Programmed":
							if condStatus == conditionTrue {
								programmed = statusYes
							} else {
								programmed = statusNo
							}
						}
					}
				}
			}
		}

		age := formatResourceAge(item.GetCreationTimestamp().Time)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n", name, className, listenerCount, accepted, programmed, age)
	}

	return nil
}

func runGetHTTPRoutes(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	list, err := dynamicClient.Resource(httpRouteGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list HTTPRoutes: %w", err)
	}

	if len(list.Items) == 0 {
		fmt.Println("No HTTPRoutes found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer func() { _ = w.Flush() }()

	_, _ = fmt.Fprintln(w, "NAME\tHOSTNAMES\tPARENTS\tACCEPTED\tAGE")

	for _, item := range list.Items {
		name := item.GetName()

		hostnames := "*"
		parentCount := 0
		if spec, ok := item.Object["spec"].(map[string]interface{}); ok {
			if hn, ok := spec["hostnames"].([]interface{}); ok && len(hn) > 0 {
				hostnames = ""
				for i, h := range hn {
					if i > 0 {
						hostnames += ","
					}
					if hs, ok := h.(string); ok {
						hostnames += hs
					}
				}
			}
			if parents, ok := spec["parentRefs"].([]interface{}); ok {
				parentCount = len(parents)
			}
		}

		accepted := httpRouteAcceptedStatus(item.Object)

		age := formatResourceAge(item.GetCreationTimestamp().Time)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", name, hostnames, parentCount, accepted, age)
	}

	return nil
}

// httpRouteAcceptedStatus extracts the accepted status from the first parent
// in an unstructured HTTPRoute object.
func httpRouteAcceptedStatus(obj map[string]interface{}) string {
	status, ok := obj["status"].(map[string]interface{})
	if !ok {
		return statusUnknown
	}
	parents, ok := status["parents"].([]interface{})
	if !ok || len(parents) == 0 {
		return statusUnknown
	}
	parent, ok := parents[0].(map[string]interface{})
	if !ok {
		return statusUnknown
	}
	conditions, ok := parent["conditions"].([]interface{})
	if !ok {
		return statusUnknown
	}
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		if condType == conditionAccepted {
			if condStatus == conditionTrue {
				return statusYes
			}
			return statusNo
		}
	}
	return statusUnknown
}

// getUnstructuredString returns a string value from an unstructured map.
func getUnstructuredString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getUnstructuredInt64 returns an int64 value from an unstructured map,
// handling both float64 and int64 representations.
func getUnstructuredInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	if v, ok := m[key].(int64); ok {
		return v
	}
	return 0
}

// printGatewaySpec prints the spec section of a Gateway.
func printGatewaySpec(spec map[string]interface{}) {
	if cn, ok := spec["gatewayClassName"].(string); ok {
		fmt.Printf("GatewayClass: %s\n", cn)
	}

	listeners, ok := spec["listeners"].([]interface{})
	if !ok {
		return
	}
	fmt.Printf("\nListeners (%d):\n", len(listeners))
	for _, l := range listeners {
		if listener, ok := l.(map[string]interface{}); ok {
			fmt.Printf("  - Name: %s, Port: %d, Protocol: %s\n",
				getUnstructuredString(listener, "name"),
				getUnstructuredInt64(listener, "port"),
				getUnstructuredString(listener, "protocol"))
		}
	}
}

// printGatewayStatus prints the status section of a Gateway.
func printGatewayStatus(status map[string]interface{}) {
	if conditions, ok := status["conditions"].([]interface{}); ok && len(conditions) > 0 {
		fmt.Printf("\nConditions:\n")
		for _, c := range conditions {
			if cond, ok := c.(map[string]interface{}); ok {
				fmt.Printf("  Type:    %s\n", getUnstructuredString(cond, "type"))
				fmt.Printf("  Status:  %s\n", getUnstructuredString(cond, "status"))
				fmt.Printf("  Reason:  %s\n", getUnstructuredString(cond, "reason"))
				fmt.Printf("  Message: %s\n", getUnstructuredString(cond, "message"))
				fmt.Println()
			}
		}
	}

	listeners, ok := status["listeners"].([]interface{})
	if !ok || len(listeners) == 0 {
		return
	}
	fmt.Printf("Listener Status:\n")
	for _, l := range listeners {
		if listener, ok := l.(map[string]interface{}); ok {
			fmt.Printf("  - Name: %s, AttachedRoutes: %d\n",
				getUnstructuredString(listener, "name"),
				getUnstructuredInt64(listener, "attachedRoutes"))
			if conditions, ok := listener["conditions"].([]interface{}); ok {
				for _, c := range conditions {
					if cond, ok := c.(map[string]interface{}); ok {
						fmt.Printf("    %s: %s\n",
							getUnstructuredString(cond, "type"),
							getUnstructuredString(cond, "status"))
					}
				}
			}
		}
	}
}

func runDescribeGatewayAPIGateway(_ *cobra.Command, args []string) error {
	ctx := context.Background()
	name := args[0]

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	gw, err := dynamicClient.Resource(gatewayGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Gateway %s: %w", name, err)
	}

	fmt.Printf("Name:         %s\n", gw.GetName())
	fmt.Printf("Namespace:    %s\n", gw.GetNamespace())
	fmt.Printf("Age:          %s\n", formatResourceAge(gw.GetCreationTimestamp().Time))

	if spec, ok := gw.Object["spec"].(map[string]interface{}); ok {
		printGatewaySpec(spec)
	}

	if status, ok := gw.Object["status"].(map[string]interface{}); ok {
		printGatewayStatus(status)
	}

	return nil
}
