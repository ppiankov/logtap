package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if err := execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func execute() error {
	// TODO: wire cobra root command with subcommands
	fmt.Printf("logtap %s\n", version)
	return nil
}
