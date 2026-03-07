package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/manifest"
	"github.com/vertrost/argoiax/pkg/output"
	"github.com/vertrost/argoiax/pkg/registry"
	"github.com/vertrost/argoiax/pkg/semver"
	"golang.org/x/sync/semaphore"
)

func newScanCmd(root *rootOptions) *cobra.Command {
	var (
		chartFilter  string
		outputFormat string
		showUpToDate bool
		failOnDrift  bool
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan for outdated Helm chart versions in ArgoCD manifests",
		Long:  `Scan scans your GitOps repository for ArgoCD Application manifests and reports which Helm charts have newer versions available.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScan(cmd.Context(), root.cfgFile, root.scanDir, chartFilter, outputFormat, showUpToDate, failOnDrift)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "output format (table, json, markdown)")
	cmd.Flags().StringVar(&chartFilter, "chart", "", "only check a specific chart name")
	cmd.Flags().BoolVar(&showUpToDate, "show-uptodate", false, "include up-to-date charts in output")
	cmd.Flags().BoolVar(&failOnDrift, "fail-on-drift", false, "exit with non-zero code when outdated charts are found")

	return cmd
}

func runScan(ctx context.Context, cfgFile, scanDir, chartFilter, outputFormat string, showUpToDate, failOnDrift bool) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	refs, err := scanRefs(cfg, scanDir, chartFilter)
	if err != nil {
		return err
	}

	if len(refs) == 0 {
		fmt.Println("No ArgoCD Helm chart references found.")
		return nil
	}

	results := checkVersions(ctx, cfg, refs)

	renderer := &output.Renderer{
		Writer:       os.Stdout,
		ShowUpToDate: showUpToDate,
	}
	if err := renderer.Render(results, outputFormat); err != nil {
		return fmt.Errorf("rendering output: %w", err)
	}

	fmt.Fprintln(os.Stderr, "\n"+output.Summary(results))

	if failOnDrift {
		for _, r := range results {
			if r.Status == output.StatusUpdateAvailable || r.Status == output.StatusBreaking {
				return errors.New("drift detected: outdated charts found")
			}
		}
	}

	return nil
}

func checkVersions(ctx context.Context, cfg *config.Config, refs []manifest.ChartReference) []output.DriftResult {
	factory := registry.NewFactory(cfg, registry.GetGitHubToken())
	results := make([]output.DriftResult, len(refs))

	const maxConcurrency = 10
	sem := semaphore.NewWeighted(maxConcurrency)
	var wg sync.WaitGroup

	for i, ref := range refs {
		results[i] = output.DriftResult{
			ChartName:      ref.ChartName,
			FilePath:       ref.FilePath,
			CurrentVersion: ref.TargetRevision,
			SourceType:     ref.Type.String(),
			RepoURL:        ref.RepoURL,
		}

		wg.Add(1)
		go func(idx int, ref manifest.ChartReference) {
			defer wg.Done()
			if err := sem.Acquire(ctx, 1); err != nil {
				slog.Error("failed to acquire semaphore", "chart", ref.ChartName, "error", err)
				results[idx].LatestVersion = "?"
				results[idx].Status = output.StatusError
				return
			}
			defer sem.Release(1)

			latest, _, _, err := resolveLatest(ctx, factory, cfg, &ref)
			if err != nil {
				slog.Error("failed to resolve latest version", "chart", ref.ChartName, "error", err)
				results[idx].LatestVersion = "?"
				results[idx].Status = output.StatusError
				return
			}

			results[idx].LatestVersion = latest

			switch {
			case semver.Equal(latest, ref.TargetRevision):
				results[idx].Status = output.StatusUpToDate
			case semver.IsMajorBump(ref.TargetRevision, latest):
				results[idx].Status = output.StatusBreaking
			default:
				results[idx].Status = output.StatusUpdateAvailable
			}
		}(i, ref)
	}

	wg.Wait()
	return results
}
