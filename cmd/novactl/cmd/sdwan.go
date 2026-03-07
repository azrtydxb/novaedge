/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

func newSDWANCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sdwan",
		Short: "SD-WAN management commands",
		Long: `Manage and inspect SD-WAN WAN links, path-selection policies,
and overlay topology.`,
	}

	cmd.AddCommand(
		newSDWANStatusCommand(),
		newSDWANLinksCommand(),
		newSDWANTopologyCommand(),
	)

	return cmd
}

func newSDWANStatusCommand() *cobra.Command {
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show SD-WAN status summary",
		Long:  "Display a summary of SD-WAN WAN links and their health status.",
		Example: `  # Show SD-WAN status
  novactl sdwan status
  novactl sdwan status -A`,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			k8sClient, err := getSDWANClient()
			if err != nil {
				return err
			}

			ns := namespace
			if allNamespaces {
				ns = ""
			}

			return runSDWANStatus(ctx, k8sClient, ns)
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Show links in all namespaces")

	return cmd
}

func newSDWANLinksCommand() *cobra.Command {
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "links",
		Short: "List SD-WAN WAN links",
		Long:  "Display a detailed table of all ProxyWANLink resources.",
		Example: `  # List all WAN links
  novactl sdwan links
  novactl sdwan links -n production`,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			k8sClient, err := getSDWANClient()
			if err != nil {
				return err
			}

			ns := namespace
			if allNamespaces {
				ns = ""
			}

			return runSDWANLinks(ctx, k8sClient, ns)
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List links in all namespaces")

	return cmd
}

func newSDWANTopologyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topology",
		Short: "Show SD-WAN overlay topology",
		Long:  "Display the SD-WAN overlay topology including sites and inter-site links.",
		Example: `  # Show topology
  novactl sdwan topology`,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			k8sClient, err := getSDWANClient()
			if err != nil {
				return err
			}

			return runSDWANTopology(ctx, k8sClient)
		},
	}

	return cmd
}

func runSDWANStatus(ctx context.Context, k8sClient client.Client, ns string) error {
	linkList := &novaedgev1alpha1.ProxyWANLinkList{}

	listOpts := []client.ListOption{}
	if ns != "" {
		listOpts = append(listOpts, client.InNamespace(ns))
	}

	if err := k8sClient.List(ctx, linkList, listOpts...); err != nil {
		return fmt.Errorf("failed to list WAN links: %w", err)
	}

	if len(linkList.Items) == 0 {
		fmt.Println("No SD-WAN links found.")
		return nil
	}

	healthy := 0
	degraded := 0
	down := 0

	for _, link := range linkList.Items {
		switch {
		case link.Status.Healthy:
			healthy++
		case link.Status.CurrentLatency != nil:
			degraded++
		default:
			down++
		}
	}

	fmt.Printf("SD-WAN Status Summary\n")
	fmt.Printf("  Total Links: %d\n", len(linkList.Items))
	fmt.Printf("  Healthy:     %d\n", healthy)
	fmt.Printf("  Degraded:    %d\n", degraded)
	fmt.Printf("  Down:        %d\n", down)

	return nil
}

func runSDWANLinks(ctx context.Context, k8sClient client.Client, ns string) error {
	linkList := &novaedgev1alpha1.ProxyWANLinkList{}

	listOpts := []client.ListOption{}
	if ns != "" {
		listOpts = append(listOpts, client.InNamespace(ns))
	}

	if err := k8sClient.List(ctx, linkList, listOpts...); err != nil {
		return fmt.Errorf("failed to list WAN links: %w", err)
	}

	if len(linkList.Items) == 0 {
		fmt.Println("No SD-WAN links found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSITE\tPROVIDER\tROLE\tBANDWIDTH\tLATENCY\tLOSS\tHEALTHY")

	for _, link := range linkList.Items {
		latency := "-"
		if link.Status.CurrentLatency != nil {
			latency = fmt.Sprintf("%.1fms", *link.Status.CurrentLatency)
		}
		loss := "-"
		if link.Status.CurrentPacketLoss != nil {
			loss = fmt.Sprintf("%.2f%%", *link.Status.CurrentPacketLoss*100)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%v\n",
			link.Name,
			link.Spec.Site,
			link.Spec.Provider,
			link.Spec.Role,
			link.Spec.Bandwidth,
			latency,
			loss,
			link.Status.Healthy,
		)
	}

	return w.Flush()
}

func runSDWANTopology(ctx context.Context, k8sClient client.Client) error {
	linkList := &novaedgev1alpha1.ProxyWANLinkList{}
	if err := k8sClient.List(ctx, linkList); err != nil {
		return fmt.Errorf("failed to list WAN links: %w", err)
	}

	if len(linkList.Items) == 0 {
		fmt.Println("No SD-WAN topology data available.")
		return nil
	}

	// Collect unique sites.
	sites := make(map[string]bool)
	for _, link := range linkList.Items {
		if link.Spec.Site != "" {
			sites[link.Spec.Site] = true
		}
	}

	fmt.Printf("SD-WAN Overlay Topology\n")
	fmt.Printf("  Sites: %d\n", len(sites))
	fmt.Printf("  Links: %d\n", len(linkList.Items))
	fmt.Println()

	if len(sites) > 0 {
		fmt.Println("Sites:")
		for site := range sites {
			fmt.Printf("  - %s\n", site)
		}
		fmt.Println()
	}

	fmt.Println("Links:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "  NAME\tSITE\tPROVIDER\tROLE\tHEALTHY")
	for _, link := range linkList.Items {
		_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%v\n",
			link.Name,
			link.Spec.Site,
			link.Spec.Provider,
			link.Spec.Role,
			link.Status.Healthy,
		)
	}

	return w.Flush()
}

// getSDWANClient creates a controller-runtime client with NovaEdge types registered.
func getSDWANClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add scheme: %w", err)
	}
	return client.New(config, client.Options{Scheme: scheme})
}
