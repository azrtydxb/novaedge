// Package cmd provides the command-line interface for novactl.
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

// newFederationCommand creates the federation command
func newFederationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "federation",
		Aliases: []string{"fed"},
		Short:   "Manage NovaEdge federation",
		Long: `Manage NovaEdge federation configuration and status.

This command allows you to view and manage active/active federation
between multiple NovaEdge controllers, including:

- Viewing federation status and peer health
- Checking sync status and conflicts
- Managing conflict resolution
- Triggering manual sync operations`,
	}

	cmd.AddCommand(newFederationStatusCommand())
	cmd.AddCommand(newFederationPeersCommand())
	cmd.AddCommand(newFederationVectorClockCommand())

	return cmd
}

// newFederationStatusCommand creates the federation status command
func newFederationStatusCommand() *cobra.Command {
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "status [NAME]",
		Short: "Show federation status",
		Long:  "Display the status of NovaEdge federation configuration.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx := context.Background()
			k8sClient, err := getClient()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				// Show specific federation
				return showFederationDetails(ctx, k8sClient, args[0], namespace)
			}

			// List all federations
			ns := namespace
			if allNamespaces {
				ns = ""
			}
			return listFederations(ctx, k8sClient, ns)
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List federations in all namespaces")

	return cmd
}

// newFederationPeersCommand creates the federation peers command
func newFederationPeersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers NAME",
		Short: "Show federation peer status",
		Long:  "Display detailed status of federation peers.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx := context.Background()
			k8sClient, err := getClient()
			if err != nil {
				return err
			}

			return showFederationPeers(ctx, k8sClient, args[0], namespace)
		},
	}

	return cmd
}

// newFederationVectorClockCommand creates the federation vector-clock command
func newFederationVectorClockCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vector-clock NAME",
		Short: "Show vector clock state",
		Long:  "Display the current vector clock state across federation members.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx := context.Background()
			k8sClient, err := getClient()
			if err != nil {
				return err
			}

			return showVectorClock(ctx, k8sClient, args[0], namespace)
		},
	}

	return cmd
}

// listFederations lists all federation resources
func listFederations(ctx context.Context, k8sClient client.Client, ns string) error {
	fedList := &novaedgev1alpha1.NovaEdgeFederationList{}

	listOpts := []client.ListOption{}
	if ns != "" {
		listOpts = append(listOpts, client.InNamespace(ns))
	}

	if err := k8sClient.List(ctx, fedList, listOpts...); err != nil {
		return fmt.Errorf("failed to list federations: %w", err)
	}

	if len(fedList.Items) == 0 {
		fmt.Println("No federation resources found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if ns == "" {
		_, _ = fmt.Fprintln(w, "NAMESPACE\tNAME\tFEDERATION ID\tLOCAL MEMBER\tPHASE\tAGE")
	} else {
		_, _ = fmt.Fprintln(w, "NAME\tFEDERATION ID\tLOCAL MEMBER\tPHASE\tAGE")
	}

	for _, fed := range fedList.Items {
		age := formatAge(fed.CreationTimestamp.Time)
		phase := string(fed.Status.Phase)
		if phase == "" {
			phase = "Unknown"
		}

		if ns == "" {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				fed.Namespace,
				fed.Name,
				fed.Spec.FederationID,
				fed.Spec.LocalMember.Name,
				phase,
				age,
			)
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				fed.Name,
				fed.Spec.FederationID,
				fed.Spec.LocalMember.Name,
				phase,
				age,
			)
		}
	}

	return w.Flush()
}

// showFederationDetails shows detailed information about a federation
func showFederationDetails(ctx context.Context, k8sClient client.Client, name, ns string) error {
	fed := &novaedgev1alpha1.NovaEdgeFederation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, fed); err != nil {
		return fmt.Errorf("failed to get federation %s: %w", name, err)
	}

	fmt.Printf("Name:           %s\n", fed.Name)
	fmt.Printf("Namespace:      %s\n", fed.Namespace)
	fmt.Printf("Federation ID:  %s\n", fed.Spec.FederationID)
	fmt.Printf("Paused:         %v\n", fed.Spec.Paused)
	fmt.Println()

	// Local member
	fmt.Println("Local Member:")
	fmt.Printf("  Name:     %s\n", fed.Spec.LocalMember.Name)
	fmt.Printf("  Endpoint: %s\n", fed.Spec.LocalMember.Endpoint)
	if fed.Spec.LocalMember.Region != "" {
		fmt.Printf("  Region:   %s\n", fed.Spec.LocalMember.Region)
	}
	if fed.Spec.LocalMember.Zone != "" {
		fmt.Printf("  Zone:     %s\n", fed.Spec.LocalMember.Zone)
	}
	fmt.Println()

	// Peers
	if len(fed.Spec.Members) > 0 {
		fmt.Println("Peers:")
		for _, peer := range fed.Spec.Members {
			fmt.Printf("  - Name:     %s\n", peer.Name)
			fmt.Printf("    Endpoint: %s\n", peer.Endpoint)
			fmt.Printf("    Priority: %d\n", peer.Priority)
			if peer.Region != "" {
				fmt.Printf("    Region:   %s\n", peer.Region)
			}
		}
		fmt.Println()
	}

	printFederationSyncConfig(fed)
	printFederationConflictConfig(fed)
	printFederationStatus(fed)
	printFederationMemberStatus(fed)
	printFederationConditions(fed)

	return nil
}

// printFederationSyncConfig prints sync configuration details.
func printFederationSyncConfig(fed *novaedgev1alpha1.NovaEdgeFederation) {
	if fed.Spec.Sync == nil {
		return
	}
	fmt.Println("Sync Configuration:")
	if fed.Spec.Sync.Interval != nil {
		fmt.Printf("  Interval:    %s\n", fed.Spec.Sync.Interval.Duration)
	}
	if fed.Spec.Sync.Timeout != nil {
		fmt.Printf("  Timeout:     %s\n", fed.Spec.Sync.Timeout.Duration)
	}
	fmt.Printf("  Batch Size:  %d\n", fed.Spec.Sync.BatchSize)
	fmt.Println()
}

// printFederationConflictConfig prints conflict resolution details.
func printFederationConflictConfig(fed *novaedgev1alpha1.NovaEdgeFederation) {
	if fed.Spec.ConflictResolution == nil {
		return
	}
	fmt.Println("Conflict Resolution:")
	fmt.Printf("  Strategy:      %s\n", fed.Spec.ConflictResolution.Strategy)
	if fed.Spec.ConflictResolution.VectorClocks != nil {
		fmt.Printf("  Vector Clocks: %v\n", *fed.Spec.ConflictResolution.VectorClocks)
	}
	if fed.Spec.ConflictResolution.TombstoneTTL != nil {
		fmt.Printf("  Tombstone TTL: %s\n", fed.Spec.ConflictResolution.TombstoneTTL.Duration)
	}
	fmt.Println()
}

// printFederationStatus prints federation status summary.
func printFederationStatus(fed *novaedgev1alpha1.NovaEdgeFederation) {
	fmt.Println("Status:")
	phase := string(fed.Status.Phase)
	if phase == "" {
		phase = "Unknown"
	}
	fmt.Printf("  Phase:             %s\n", phase)
	if fed.Status.LastSyncTime != nil {
		fmt.Printf("  Last Sync:         %s\n", fed.Status.LastSyncTime.Format(time.RFC3339))
	}
	if fed.Status.SyncLag != nil {
		fmt.Printf("  Sync Lag:          %s\n", fed.Status.SyncLag.Duration)
	}
	fmt.Printf("  Conflicts Pending: %d\n", fed.Status.ConflictsPending)
	fmt.Println()
}

// printFederationMemberStatus prints federation member status table.
func printFederationMemberStatus(fed *novaedgev1alpha1.NovaEdgeFederation) {
	if len(fed.Status.Members) == 0 {
		return
	}
	fmt.Println("Member Status:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "  NAME\tHEALTHY\tLAST SEEN\tSYNC LAG\tAGENTS\tERROR")
	for _, member := range fed.Status.Members {
		lastSeen := "Never"
		if member.LastSeen != nil {
			lastSeen = formatAge(member.LastSeen.Time)
		}
		syncLag := "-"
		if member.SyncLag != nil {
			syncLag = member.SyncLag.Duration.String()
		}
		errStr := member.Error
		if len(errStr) > 40 {
			errStr = errStr[:37] + "..."
		}
		_, _ = fmt.Fprintf(w, "  %s\t%v\t%s\t%s\t%d\t%s\n",
			member.Name, member.Healthy, lastSeen, syncLag, member.AgentCount, errStr)
	}
	_ = w.Flush()
	fmt.Println()
}

// printFederationConditions prints federation conditions.
func printFederationConditions(fed *novaedgev1alpha1.NovaEdgeFederation) {
	if len(fed.Status.Conditions) == 0 {
		return
	}
	fmt.Println("Conditions:")
	for _, cond := range fed.Status.Conditions {
		fmt.Printf("  - Type:    %s\n", cond.Type)
		fmt.Printf("    Status:  %s\n", cond.Status)
		fmt.Printf("    Reason:  %s\n", cond.Reason)
		fmt.Printf("    Message: %s\n", cond.Message)
	}
}

// showFederationPeers shows peer details
func showFederationPeers(ctx context.Context, k8sClient client.Client, name, ns string) error {
	fed := &novaedgev1alpha1.NovaEdgeFederation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, fed); err != nil {
		return fmt.Errorf("failed to get federation %s: %w", name, err)
	}

	if len(fed.Spec.Members) == 0 {
		fmt.Println("No peers configured")
		return nil
	}

	// Build status map
	statusMap := make(map[string]*novaedgev1alpha1.FederationMemberStatus)
	for i := range fed.Status.Members {
		statusMap[fed.Status.Members[i].Name] = &fed.Status.Members[i]
	}

	for _, peer := range fed.Spec.Members {
		fmt.Printf("Peer: %s\n", peer.Name)
		fmt.Printf("  Endpoint: %s\n", peer.Endpoint)
		fmt.Printf("  Priority: %d\n", peer.Priority)
		if peer.Region != "" {
			fmt.Printf("  Region:   %s\n", peer.Region)
		}
		if peer.Zone != "" {
			fmt.Printf("  Zone:     %s\n", peer.Zone)
		}

		if peer.TLS != nil {
			tlsEnabled := peer.TLS.Enabled == nil || *peer.TLS.Enabled
			fmt.Printf("  TLS:      %v\n", tlsEnabled)
		}

		if status, ok := statusMap[peer.Name]; ok {
			fmt.Println("  Status:")
			fmt.Printf("    Healthy:     %v\n", status.Healthy)
			if status.LastSeen != nil {
				fmt.Printf("    Last Seen:   %s ago\n", formatAge(status.LastSeen.Time))
			}
			if status.LastSyncTime != nil {
				fmt.Printf("    Last Sync:   %s ago\n", formatAge(status.LastSyncTime.Time))
			}
			if status.SyncLag != nil {
				fmt.Printf("    Sync Lag:    %s\n", status.SyncLag.Duration)
			}
			fmt.Printf("    Agent Count: %d\n", status.AgentCount)
			if status.Error != "" {
				fmt.Printf("    Error:       %s\n", status.Error)
			}

			// Vector clock
			if len(status.VectorClock) > 0 {
				fmt.Println("    Vector Clock:")
				for member, value := range status.VectorClock {
					fmt.Printf("      %s: %d\n", member, value)
				}
			}
		} else {
			fmt.Println("  Status: Unknown (not yet connected)")
		}

		fmt.Println()
	}

	return nil
}

// showVectorClock shows the vector clock state
func showVectorClock(ctx context.Context, k8sClient client.Client, name, ns string) error {
	fed := &novaedgev1alpha1.NovaEdgeFederation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, fed); err != nil {
		return fmt.Errorf("failed to get federation %s: %w", name, err)
	}

	fmt.Printf("Vector Clock State for Federation: %s\n", name)
	fmt.Println(strings.Repeat("-", 50))

	// Local vector clock
	fmt.Println("Local Controller:")
	if len(fed.Status.LocalVectorClock) > 0 {
		for member, value := range fed.Status.LocalVectorClock {
			fmt.Printf("  %s: %d\n", member, value)
		}
	} else {
		fmt.Println("  (no data)")
	}
	fmt.Println()

	// Peer vector clocks
	if len(fed.Status.Members) > 0 {
		fmt.Println("Peer Vector Clocks:")
		for _, member := range fed.Status.Members {
			fmt.Printf("  %s:\n", member.Name)
			if len(member.VectorClock) > 0 {
				for m, v := range member.VectorClock {
					fmt.Printf("    %s: %d\n", m, v)
				}
			} else {
				fmt.Println("    (no data)")
			}
		}
	}

	return nil
}

// getClient creates a controller-runtime client
func getClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	return client.New(config, client.Options{Scheme: scheme})
}

// formatAge formats a time as a human-readable age string
func formatAge(t time.Time) string {
	d := time.Since(t)
	if d.Hours() >= 24 {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
	if d.Hours() >= 1 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d.Minutes() >= 1 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
