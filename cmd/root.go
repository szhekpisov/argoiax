package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// opts holds all CLI flag values in a single struct.
var opts struct {
	// root flags
	cfgFile  string
	scanDir  string
	dryRun   bool
	logLevel string

	// shared between scan and update
	chartFilter string

	// scan flags
	outputFormat string
	showUpToDate bool

	// update flags
	allowMajor  bool
	maxPRs      int
	githubToken string
	repoSlug    string
}

var rootCmd = &cobra.Command{
	Use:   "ancaeus",
	Short: "Detect and update outdated Helm chart versions in ArgoCD manifests",
	Long: `ancaeus scans your GitOps repository for ArgoCD Application manifests,
detects outdated Helm chart versions across HTTP, OCI, and Git repositories,
fetches release notes with breaking change detection, and opens PRs for updates.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setupLogging()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&opts.cfgFile, "config", "", "config file (default: ancaeus.yaml)")
	rootCmd.PersistentFlags().StringVar(&opts.scanDir, "dir", "", "directory to scan (overrides config scanDirs)")
	rootCmd.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", false, "report changes without modifying files")
	rootCmd.PersistentFlags().StringVar(&opts.logLevel, "log-level", "info", "log level (debug, info, warn, error)")
}

func setupLogging() {
	var level slog.Level
	if err := level.UnmarshalText([]byte(opts.logLevel)); err != nil {
		level = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
