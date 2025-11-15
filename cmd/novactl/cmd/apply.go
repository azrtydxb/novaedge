package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var filename string

func newApplyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply NovaEdge resources from a file",
		Long:  `Create or update NovaEdge resources from YAML or JSON files.`,
		Example: `  # Apply a single resource from a file
  novactl apply -f gateway.yaml

  # Apply multiple resources
  novactl apply -f config/samples/`,
		RunE: runApply,
	}

	cmd.Flags().StringVarP(&filename, "filename", "f", "", "File or directory containing resource definitions")
	cmd.MarkFlagRequired("filename")

	return cmd
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Read file
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	// Parse YAML (could contain multiple documents)
	var obj unstructured.Unstructured
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Apply resource
	result, err := c.ApplyResource(ctx, &obj)
	if err != nil {
		return fmt.Errorf("failed to apply resource: %w", err)
	}

	kind := result.GetKind()
	name := result.GetName()
	fmt.Printf("%s/%s configured\n", kind, name)

	return nil
}
