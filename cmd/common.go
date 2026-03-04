package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/vertrost/ancaeus/pkg/config"
	"github.com/vertrost/ancaeus/pkg/manifest"
	"github.com/vertrost/ancaeus/pkg/registry"
	"github.com/vertrost/ancaeus/pkg/semver"
)

// scanRefs resolves scan directories, walks manifests, and optionally filters by chart name.
func scanRefs(cfg *config.Config, dir, chart string) ([]manifest.ChartReference, error) {
	dirs := cfg.ScanDirs
	if dir != "" {
		dirs = []string{dir}
	}
	resolved := make([]string, len(dirs))
	for i, d := range dirs {
		resolved[i] = filepath.Clean(d)
	}

	slog.Info("scanning directories", "dirs", resolved)

	walker := &manifest.Walker{IgnorePatterns: cfg.Ignore}
	refs, err := walker.Walk(resolved)
	if err != nil {
		return nil, fmt.Errorf("scanning manifests: %w", err)
	}

	if chart != "" {
		filtered := make([]manifest.ChartReference, 0, len(refs))
		for _, r := range refs {
			if r.ChartName == chart {
				filtered = append(filtered, r)
			}
		}
		refs = filtered
	}

	slog.Info("found chart references", "count", len(refs))
	return refs, nil
}

// resolveLatest gets the registry for a ref, lists versions, and resolves the latest one.
func resolveLatest(ctx context.Context, factory *registry.Factory, cfg *config.Config, ref manifest.ChartReference) (latest string, allVersions []string, chartCfg *config.Chart, err error) {
	reg, err := factory.GetRegistry(ref)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to get registry: %w", err)
	}

	allVersions, err = reg.ListVersions(ctx, ref)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to list versions: %w", err)
	}

	chartCfg = cfg.LookupChart(ref.ChartName, ref.RepoURL)
	constraint := ""
	if chartCfg != nil {
		constraint = chartCfg.VersionConstraint
	}

	latest, err = semver.LatestStable(allVersions, constraint)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolving latest version for %s: %w", ref.ChartName, err)
	}
	if latest == "" {
		return "", nil, nil, fmt.Errorf("no valid versions found for %s", ref.ChartName)
	}

	return latest, allVersions, chartCfg, nil
}
