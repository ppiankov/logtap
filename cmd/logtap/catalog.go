package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newCatalogCmd() *cobra.Command {
	var (
		jsonOutput bool
		recursive  bool
	)

	cmd := &cobra.Command{
		Use:   "catalog [dir]",
		Short: "Discover and list capture directories",
		Long:  "Scan a directory for logtap captures (directories containing metadata.json) and list them with summary information.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = args[0]
			}
			return runCatalog(root, recursive, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "scan subdirectories recursively")

	return cmd
}

func runCatalog(root string, recursive, jsonOutput bool) error {
	entries, err := archive.Catalog(root, recursive)
	if err != nil {
		return err
	}

	if jsonOutput {
		return archive.WriteCatalogJSON(os.Stdout, entries)
	}

	archive.WriteCatalogText(os.Stdout, entries)
	return nil
}
