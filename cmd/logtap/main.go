package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func execute() error {
	root := &cobra.Command{
		Use:     "logtap",
		Short:   "Ephemeral log mirror for load testing",
		Version: version,
	}
	root.AddCommand(newRecvCmd())
	return root.Execute()
}
