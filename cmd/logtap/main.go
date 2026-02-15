package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/config"
)

var (
	version = "dev"
	cfg     *config.Config
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func execute() error {
	cfg = config.Load()

	root := &cobra.Command{
		Use:     "logtap",
		Short:   "Ephemeral log mirror for load testing",
		Version: version,
	}
	root.AddCommand(newRecvCmd())
	root.AddCommand(newOpenCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newSliceCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newTriageCmd())
	root.AddCommand(newGrepCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newSnapshotCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newTapCmd())
	root.AddCommand(newUntapCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newStatusCmd())
	return root.Execute()
}
