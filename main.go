package main

import (
	"os"

	"github.com/vertrost/ancaeus/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
