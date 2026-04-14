package main

import (
	"fmt"
	"os"

	"moodletodo/internal/cli"
)

func main() {
	root, err := cli.NewRootCmd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to initialize CLI:", err)
		os.Exit(1)
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
