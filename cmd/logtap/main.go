package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/cli"
	"github.com/ppiankov/logtap/internal/config"
)

const defaultTimeout = 30 * time.Second

var (
	version    = "dev"
	commit     = "none"
	date       = "unknown"
	cfg        *config.Config
	timeoutStr string
)

type buildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
}

func main() {
	if err := execute(); err != nil {
		jsonMode := hasJSONFlag(os.Args)
		cli.FormatError(os.Stderr, err, jsonMode)
		os.Exit(cli.ExitCode(err))
	}
}

// hasJSONFlag checks if --json appears in the command-line arguments.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
	}
	return false
}

func execute() error {
	cfg = config.Load()

	root := &cobra.Command{
		Use:   "logtap",
		Short: "Ephemeral log mirror for load testing",
	}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout for cluster operations (e.g. 30s, 1m)")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newRecvCmd())
	root.AddCommand(newOpenCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newGCCmd())
	root.AddCommand(newSliceCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newTriageCmd())
	root.AddCommand(newGrepCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newSnapshotCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newUploadCmd())
	root.AddCommand(newDownloadCmd())
	root.AddCommand(newTapCmd())
	root.AddCommand(newUntapCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newDeployCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newCatalogCmd())
	root.AddCommand(newReportCmd())
	return root.Execute()
}

func newVersionCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			info := buildInfo{
				Version:   version,
				Commit:    commit,
				Date:      date,
				GoVersion: runtime.Version(),
			}
			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				_ = enc.Encode(info)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "logtap %s (commit: %s, built: %s, go: %s)\n",
					info.Version, info.Commit, info.Date, info.GoVersion)
			}
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output version as JSON")
	return cmd
}
