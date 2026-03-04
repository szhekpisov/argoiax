package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"github.com/vertrost/ancaeus/pkg/config"
	"github.com/vertrost/ancaeus/pkg/output"
	"github.com/vertrost/ancaeus/pkg/pr"
	"github.com/vertrost/ancaeus/pkg/registry"
	"github.com/vertrost/ancaeus/pkg/releasenotes"
	"github.com/vertrost/ancaeus/pkg/semver"
	"github.com/vertrost/ancaeus/pkg/updater"
	"golang.org/x/oauth2"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Create PRs for outdated Helm chart versions",
	Long:  `Update scans for outdated charts, modifies YAML files, and creates pull requests on GitHub.`,
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&opts.chartFilter, "chart", "", "only update a specific chart name")
	updateCmd.Flags().BoolVar(&opts.allowMajor, "allow-major", false, "include major version updates")
	updateCmd.Flags().IntVar(&opts.maxPRs, "max-prs", 0, "maximum number of PRs to create (0 = use config)")
	updateCmd.Flags().StringVar(&opts.githubToken, "github-token", "", "GitHub token (or set GITHUB_TOKEN env var)")
	updateCmd.Flags().StringVar(&opts.repoSlug, "repo", "", "GitHub repository (owner/repo)")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cfg, err := config.Load(opts.cfgFile)
	if err != nil {
		return err
	}

	// Resolve token
	token := opts.githubToken
	if token == "" {
		token = registry.GetGitHubToken()
	}
	if token == "" && !opts.dryRun {
		return fmt.Errorf("GitHub token required (use --github-token or set GITHUB_TOKEN)")
	}

	// Resolve repo slug
	owner, repo, err := resolveRepo(opts.repoSlug)
	if err != nil && !opts.dryRun {
		return err
	}

	refs, err := scanRefs(cfg, opts.scanDir, opts.chartFilter)
	if err != nil {
		return err
	}

	// Check versions and create PRs
	factory := registry.NewFactory(cfg, token)

	maxPRCount := opts.maxPRs
	if maxPRCount == 0 {
		maxPRCount = cfg.Settings.MaxOpenPRs
	}

	var prCreator pr.Creator
	if !opts.dryRun {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(ctx, ts)
		ghClient := github.NewClient(tc)
		prCreator = pr.NewGitHubCreator(ghClient, owner, repo, cfg.Settings)
	}

	// Initialize release notes orchestrator
	notesOrch := releasenotes.NewOrchestrator(cfg.Release, token)

	prsCreated := 0
	for _, ref := range refs {
		if maxPRCount > 0 && prsCreated >= maxPRCount {
			slog.Info("reached max PR limit", "limit", maxPRCount)
			break
		}

		latest, allVersions, chartCfg, err := resolveLatest(ctx, factory, cfg, ref)
		if err != nil {
			slog.Error("failed to resolve latest version", "chart", ref.ChartName, "error", err)
			continue
		}

		if latest == ref.TargetRevision {
			continue
		}

		isMajor := semver.IsMajorBump(ref.TargetRevision, latest)
		if isMajor && !opts.allowMajor {
			slog.Info("skipping major update", "chart", ref.ChartName, "current", ref.TargetRevision, "latest", latest)
			continue
		}

		slog.Info("update available", "chart", ref.ChartName, "current", ref.TargetRevision, "latest", latest)

		// Check for existing PR before doing expensive work
		if prCreator != nil {
			branch, err := pr.RenderTemplate(cfg.Settings.BranchTemplate, pr.UpdateInfo{
				ChartName:  ref.ChartName,
				OldVersion: ref.TargetRevision,
				NewVersion: latest,
			})
			if err != nil {
				slog.Error("failed to render branch template", "chart", ref.ChartName, "error", err)
				continue
			}
			exists, err := prCreator.ExistingPR(ctx, branch)
			if err != nil {
				slog.Warn("error checking existing PR", "chart", ref.ChartName, "error", err)
			}
			if exists {
				slog.Info("PR already exists, skipping", "chart", ref.ChartName, "branch", branch)
				continue
			}
		}

		// Fetch release notes
		var versionsToFetch []string
		if cfg.Release.IncludeIntermediate {
			versionsToFetch = semver.VersionsBetween(allVersions, ref.TargetRevision, latest)
		}
		versionsToFetch = append(versionsToFetch, latest)
		notes := notesOrch.FetchNotes(ctx, ref.ChartName, ref.RepoURL, versionsToFetch, chartCfg)

		// Detect breaking changes
		breakingResult := semver.DetectBreaking(ref.TargetRevision, latest, notes.CombinedBody())

		if opts.dryRun {
			status := output.StatusUpdateAvailable
			if breakingResult.IsBreaking {
				status = output.StatusBreaking
			}
			fmt.Printf("[DRY-RUN] Would update %s in %s: %s → %s (%s)\n",
				ref.ChartName, ref.FilePath, ref.TargetRevision, latest, status)
			continue
		}

		// Update the YAML file
		fileData, err := os.ReadFile(ref.FilePath)
		if err != nil {
			slog.Error("failed to read file", "path", ref.FilePath, "error", err)
			continue
		}

		updatedData, err := updater.UpdateBytes(fileData, ref, latest)
		if err != nil {
			slog.Error("failed to update YAML", "chart", ref.ChartName, "error", err)
			continue
		}

		// Create PR
		info := pr.UpdateInfo{
			ChartName:       ref.ChartName,
			OldVersion:      ref.TargetRevision,
			NewVersion:      latest,
			FilePath:        ref.FilePath,
			RepoURL:         ref.RepoURL,
			IsBreaking:      breakingResult.IsBreaking,
			BreakingReasons: breakingResult.Reasons,
			ReleaseNotes:    notes,
		}

		result, err := prCreator.CreatePR(ctx, info, updatedData, cfg.Settings.BaseBranch)
		if err != nil {
			slog.Error("failed to create PR", "chart", ref.ChartName, "error", err)
			continue
		}

		fmt.Printf("Created PR: %s\n", result.PRURL)
		prsCreated++
	}

	if prsCreated == 0 && !opts.dryRun {
		fmt.Println("No updates to create PRs for.")
	} else if prsCreated > 0 {
		fmt.Printf("\nCreated %d PR(s).\n", prsCreated)
	}

	return nil
}

func resolveRepo(slug string) (string, string, error) {
	if slug == "" {
		slug = os.Getenv("GITHUB_REPOSITORY")
	}
	if slug == "" {
		return "", "", fmt.Errorf("repository not specified (use --repo or set GITHUB_REPOSITORY)")
	}

	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository format %q (expected owner/repo)", slug)
	}
	return parts[0], parts[1], nil
}

