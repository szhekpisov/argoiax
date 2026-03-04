package main

import (
	"os"

	"github.com/vertrost/argoiax/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
