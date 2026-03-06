package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build-time variables set by -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of argoiax",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("argoiax %s (commit: %s, built: %s)\n", Version, Commit, Date)
		},
	}
}
