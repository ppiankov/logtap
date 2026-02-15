package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/config"
)

const defaultTimeout = 30 * time.Second

var (
	version    = "dev"
	cfg        *config.Config
	timeoutStr string
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
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout for cluster operations (e.g. 30s, 1m)")
	root.AddCommand(newRecvCmd())
	root.AddCommand(newOpenCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newSliceCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newTriageCmd())
	root.AddCommand(newGrepCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newSnapshotCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newTapCmd())
	root.AddCommand(newUntapCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newStatusCmd())
	return root.Execute()
}
