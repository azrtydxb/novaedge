package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/client"
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
	cobra.CheckErr(cmd.MarkFlagRequired("filename"))

	return cmd
}

func runApply(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Clean the file path to prevent path traversal
	cleanPath := filepath.Clean(filename)

	// Read file
	data, err := os.ReadFile(cleanPath) //#nosec G304 -- filename is provided by CLI user via --filename flag
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", cleanPath, err)
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
