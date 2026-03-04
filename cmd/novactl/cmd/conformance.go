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

func newConformanceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conformance",
		Short: "Check Gateway API conformance status",
		Long: `Display the Gateway API conformance status of the NovaEdge installation.
This command checks GatewayClass acceptance, Gateway status, and HTTPRoute
conditions to verify the controller is operating correctly.

For full conformance testing against the official test suite, run:
  make test-conformance`,
		RunE: runConformance,
	}

	return cmd
}

// checkGatewayClasses checks and prints GatewayClass status.
func checkGatewayClasses(ctx context.Context, dynamicClient dynamic.Interface) {
	fmt.Println("GatewayClass Status:")
	gcGVR := schema.GroupVersionResource{
		Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses",
	}

	gcList, err := dynamicClient.Resource(gcGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  ERROR: Failed to list GatewayClasses: %v\n", err)
		fmt.Println("  Ensure Gateway API CRDs are installed:")
		fmt.Println("    kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml")
		fmt.Println()
		return
	}

	found := false
	for _, gc := range gcList.Items {
		controllerName := ""
		if spec, ok := gc.Object["spec"].(map[string]interface{}); ok {
			controllerName, _ = spec["controllerName"].(string)
		}
		if controllerName != "novaedge.io/gateway-controller" {
			continue
		}
		found = true
		fmt.Printf("  Name: %s\n", gc.GetName())
		fmt.Printf("  Controller: %s\n", controllerName)
		printStatusConditions(gc.Object)
	}

	if !found {
		fmt.Println("  WARNING: No NovaEdge GatewayClass found")
		fmt.Println("  Apply the GatewayClass: kubectl apply -f config/samples/gatewayclass.yaml")
	}
	fmt.Println()
}

// printStatusConditions prints conditions from a resource's status.
func printStatusConditions(obj map[string]interface{}) {
	status, ok := obj["status"].(map[string]interface{})
	if !ok {
		return
	}
	conditions, ok := status["conditions"].([]interface{})
	if !ok {
		return
	}
	for _, c := range conditions {
		if cond, ok := c.(map[string]interface{}); ok {
			condType, _ := cond["type"].(string)
			condStatus, _ := cond["status"].(string)
			condReason, _ := cond["reason"].(string)
			fmt.Printf("  %s: %s (%s)\n", condType, condStatus, condReason)
		}
	}
}

// extractConditionStatus extracts condition values from a conditions list.
func extractConditionStatus(conditions []interface{}, targets map[string]*string) {
	for _, c := range conditions {
		if cond, ok := c.(map[string]interface{}); ok {
			condType, _ := cond["type"].(string)
			condStatus, _ := cond["status"].(string)
			if ptr, exists := targets[condType]; exists {
				*ptr = condStatus
			}
		}
	}
}

// checkGateways checks and prints Gateway status.
func checkGateways(ctx context.Context, dynamicClient dynamic.Interface) {
	fmt.Println("Gateway Status:")
	gwGVR := schema.GroupVersionResource{
		Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways",
	}

	gwList, err := dynamicClient.Resource(gwGVR).Namespace("").List(ctx, metav1.ListOptions{})
	switch {
	case err != nil:
		fmt.Printf("  ERROR: Failed to list Gateways: %v\n", err)
	case len(gwList.Items) == 0:
		fmt.Println("  No Gateways found")
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "  NAME\tNAMESPACE\tCLASS\tACCEPTED\tPROGRAMMED")
		for _, gw := range gwList.Items {
			className := ""
			if spec, ok := gw.Object["spec"].(map[string]interface{}); ok {
				className, _ = spec["gatewayClassName"].(string)
			}
			accepted, programmed := "-", "-"
			if status, ok := gw.Object["status"].(map[string]interface{}); ok {
				if conditions, ok := status["conditions"].([]interface{}); ok {
					extractConditionStatus(conditions, map[string]*string{
						"Accepted": &accepted, "Programmed": &programmed,
					})
				}
			}
			_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
				gw.GetName(), gw.GetNamespace(), className, accepted, programmed)
		}
		_ = w.Flush()
	}
	fmt.Println()
}

// checkHTTPRoutes checks and prints HTTPRoute status.
func checkHTTPRoutes(ctx context.Context, dynamicClient dynamic.Interface) {
	fmt.Println("HTTPRoute Status:")
	hrGVR := schema.GroupVersionResource{
		Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes",
	}

	hrList, err := dynamicClient.Resource(hrGVR).Namespace("").List(ctx, metav1.ListOptions{})
	switch {
	case err != nil:
		fmt.Printf("  ERROR: Failed to list HTTPRoutes: %v\n", err)
	case len(hrList.Items) == 0:
		fmt.Println("  No HTTPRoutes found")
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "  NAME\tNAMESPACE\tACCEPTED\tRESOLVED_REFS")
		for _, hr := range hrList.Items {
			accepted, resolvedRefs := "-", "-"
			if status, ok := hr.Object["status"].(map[string]interface{}); ok {
				if parents, ok := status["parents"].([]interface{}); ok && len(parents) > 0 {
					if parent, ok := parents[0].(map[string]interface{}); ok {
						if conditions, ok := parent["conditions"].([]interface{}); ok {
							extractConditionStatus(conditions, map[string]*string{
								"Accepted": &accepted, "ResolvedRefs": &resolvedRefs,
							})
						}
					}
				}
			}
			_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
				hr.GetName(), hr.GetNamespace(), accepted, resolvedRefs)
		}
		_ = w.Flush()
	}
	fmt.Println()
}

// printSupportedFeatures prints the supported conformance features summary.
func printSupportedFeatures() {
	fmt.Println("Supported Conformance Profiles:")
	fmt.Println("  GATEWAY-HTTP: Core + Extended")
	fmt.Println()
	fmt.Println("Supported Core Features:")
	fmt.Println("  - Gateway")
	fmt.Println("  - HTTPRoute")
	fmt.Println("  - HTTPRouteHostRewrite")
	fmt.Println("  - HTTPRoutePathRewrite")
	fmt.Println("  - HTTPRoutePathRedirect")
	fmt.Println("  - HTTPRouteSchemeRedirect")
	fmt.Println("  - HTTPRouteRequestHeaderModification")
	fmt.Println("  - HTTPRouteResponseHeaderModification")
	fmt.Println("  - HTTPRouteRequestMirror")
	fmt.Println()
	fmt.Println("Supported Extended Features:")
	fmt.Println("  - GatewayPort8080")
	fmt.Println("  - GatewayHTTPListenerIsolation")
	fmt.Println()
	fmt.Println("Run full conformance tests: make test-conformance")
}

func runConformance(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	fmt.Println("NovaEdge Gateway API Conformance Status")
	fmt.Println("========================================")
	fmt.Println()

	checkGatewayClasses(ctx, dynamicClient)
	checkGateways(ctx, dynamicClient)
	checkHTTPRoutes(ctx, dynamicClient)
	printSupportedFeatures()

	return nil
}
