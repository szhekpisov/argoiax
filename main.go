// Package main is the entry point for the argoiax CLI.
package main

import (
	"os"

	"github.com/szhekpisov/argoiax/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
