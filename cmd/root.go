package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// rootOptions holds flags shared across all subcommands.
type rootOptions struct {
	cfgFile  string
	scanDir  string
	dryRun   bool
	logLevel string
}

// Execute runs the root CLI command.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	var o rootOptions

	cmd := &cobra.Command{
		Use:   "argoiax",
		Short: "Detect and update outdated Helm chart versions in ArgoCD manifests",
		Long: `argoiax scans your GitOps repository for ArgoCD Application manifests,
detects outdated Helm chart versions across HTTP, OCI, and Git repositories,
fetches release notes with breaking change detection, and opens PRs for updates.`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			setupLogging(o.logLevel)
		},
	}

	cmd.PersistentFlags().StringVar(&o.cfgFile, "config", "", "config file (default: argoiax.yaml)")
	cmd.PersistentFlags().StringVar(&o.scanDir, "dir", "", "directory to scan (overrides config scanDirs)")
	cmd.PersistentFlags().BoolVar(&o.dryRun, "dry-run", false, "report changes without modifying files")
	cmd.PersistentFlags().StringVar(&o.logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	cmd.AddCommand(newScanCmd(&o))
	cmd.AddCommand(newUpdateCmd(&o))
	cmd.AddCommand(newVersionCmd())

	return cmd
}

func setupLogging(logLevel string) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		level = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
