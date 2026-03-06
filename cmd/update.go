package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/manifest"
	"github.com/vertrost/argoiax/pkg/output"
	"github.com/vertrost/argoiax/pkg/pr"
	"github.com/vertrost/argoiax/pkg/registry"
	"github.com/vertrost/argoiax/pkg/releasenotes"
	"github.com/vertrost/argoiax/pkg/semver"
	"github.com/vertrost/argoiax/pkg/updater"
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

// resolvedUpdate holds a resolved chart update with all metadata needed for PR creation.
type resolvedUpdate struct {
	ref  manifest.ChartReference
	info pr.UpdateInfo
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	cfg, err := config.Load(opts.cfgFile)
	if err != nil {
		return err
	}

	token, owner, repo, err := resolveCredentials()
	if err != nil {
		return err
	}

	refs, err := scanRefs(cfg, opts.scanDir, opts.chartFilter)
	if err != nil {
		return err
	}

	updates := resolveUpdates(ctx, cfg, refs,
		registry.NewFactory(cfg, token),
		releasenotes.NewOrchestrator(cfg.Release, token))

	if len(updates) == 0 {
		if !opts.dryRun {
			fmt.Println("No updates to create PRs for.")
		}
		return nil
	}

	if opts.dryRun {
		printDryRun(updates)
		return nil
	}

	return createPRs(ctx, cfg, token, owner, repo, updates)
}

func resolveCredentials() (token, owner, repo string, err error) {
	token = opts.githubToken
	if token == "" {
		token = registry.GetGitHubToken()
	}
	if token == "" && !opts.dryRun {
		return "", "", "", errors.New("GitHub token required (use --github-token or set GITHUB_TOKEN)")
	}

	owner, repo, err = resolveRepo(opts.repoSlug)
	if err != nil && !opts.dryRun {
		return "", "", "", err
	}
	return token, owner, repo, nil
}

func printDryRun(updates []resolvedUpdate) {
	for i := range updates {
		status := output.StatusUpdateAvailable
		if updates[i].info.IsBreaking {
			status = output.StatusBreaking
		}
		fmt.Printf("[DRY-RUN] Would update %s in %s: %s → %s (%s)\n",
			updates[i].info.ChartName, updates[i].info.FilePath, updates[i].info.OldVersion, updates[i].info.NewVersion, status)
	}
}

func createPRs(ctx context.Context, cfg *config.Config, token, owner, repo string, updates []resolvedUpdate) error {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)
	prCreator := pr.NewGitHubCreator(ghClient, owner, repo, &cfg.Settings)

	maxPRCount := opts.maxPRs
	if maxPRCount == 0 {
		maxPRCount = cfg.Settings.MaxOpenPRs
	}

	var prsCreated int
	var err error
	switch cfg.Settings.PRStrategy {
	case config.StrategyPerFile:
		prsCreated = createPerFilePRs(ctx, &cfg.Settings, updates, prCreator, maxPRCount)
	case config.StrategyBatch:
		prsCreated, err = createBatchPR(ctx, &cfg.Settings, updates, prCreator)
		if err != nil {
			return err
		}
	default:
		prsCreated = createPerChartPRs(ctx, &cfg.Settings, updates, prCreator, maxPRCount)
	}

	if prsCreated == 0 {
		fmt.Println("No updates to create PRs for.")
	} else {
		fmt.Printf("\nCreated %d PR(s).\n", prsCreated)
	}
	return nil
}

// resolveUpdates resolves version updates for all refs, fetches release notes, and detects breaking changes.
func resolveUpdates(ctx context.Context, cfg *config.Config, refs []manifest.ChartReference, factory *registry.Factory, notesOrch *releasenotes.Orchestrator) []resolvedUpdate {
	var updates []resolvedUpdate

	for i := range refs {
		ref := &refs[i]
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

		// Fetch release notes
		var versionsToFetch []string
		if cfg.Release.IncludeIntermediate {
			versionsToFetch = semver.VersionsBetween(allVersions, ref.TargetRevision, latest)
		}
		versionsToFetch = append(versionsToFetch, latest)
		notes := notesOrch.FetchNotes(ctx, ref.ChartName, ref.RepoURL, versionsToFetch, chartCfg)

		// Detect breaking changes
		breakingResult := semver.DetectBreaking(ref.TargetRevision, latest, notes.CombinedBody())

		updates = append(updates, resolvedUpdate{
			ref: *ref,
			info: pr.UpdateInfo{
				ChartName:       ref.ChartName,
				OldVersion:      ref.TargetRevision,
				NewVersion:      latest,
				FilePath:        ref.FilePath,
				RepoURL:         ref.RepoURL,
				IsBreaking:      breakingResult.IsBreaking,
				BreakingReasons: breakingResult.Reasons,
				ReleaseNotes:    notes,
			},
		})
	}

	return updates
}

// createPerChartPRs creates one PR per chart update (existing behavior).
func createPerChartPRs(ctx context.Context, settings *config.Settings, updates []resolvedUpdate, prCreator pr.Creator, maxPRCount int) int {
	prsCreated := 0

	for i := range updates {
		if maxPRCount > 0 && prsCreated >= maxPRCount {
			slog.Info("reached max PR limit", "limit", maxPRCount)
			break
		}

		// Check for existing PR
		branch, err := pr.RenderTemplate(settings.BranchTemplate, updates[i].info)
		if err != nil {
			slog.Error("failed to render branch template", "chart", updates[i].info.ChartName, "error", err)
			continue
		}
		exists, err := prCreator.ExistingPR(ctx, branch)
		if err != nil {
			slog.Warn("error checking existing PR", "chart", updates[i].info.ChartName, "error", err)
		}
		if exists {
			slog.Info("PR already exists, skipping", "chart", updates[i].info.ChartName, "branch", branch)
			continue
		}

		// Read and update the file
		updatedData, err := applyFileUpdates([]resolvedUpdate{updates[i]})
		if err != nil {
			slog.Error("failed to apply file update", "chart", updates[i].info.ChartName, "error", err)
			continue
		}

		result, err := prCreator.CreatePR(ctx, &updates[i].info, updatedData, settings.BaseBranch)
		if err != nil {
			slog.Error("failed to create PR", "chart", updates[i].info.ChartName, "error", err)
			continue
		}

		fmt.Printf("Created PR: %s\n", result.PRURL)
		prsCreated++
	}

	return prsCreated
}

// groupByFile groups resolved updates by file path, returning both the groups and ordered keys in a single pass.
func groupByFile(updates []resolvedUpdate) (groups map[string][]resolvedUpdate, keys []string) {
	groups = make(map[string][]resolvedUpdate)
	for i := range updates {
		fp := updates[i].info.FilePath
		if _, exists := groups[fp]; !exists {
			keys = append(keys, fp)
		}
		groups[fp] = append(groups[fp], updates[i])
	}
	return groups, keys
}

// applyFileUpdates reads a file and applies all chart updates in sequence, returning the final content.
func applyFileUpdates(fileUpdates []resolvedUpdate) ([]byte, error) {
	if len(fileUpdates) == 0 {
		return nil, errors.New("no updates to apply")
	}

	data, err := os.ReadFile(fileUpdates[0].ref.FilePath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", fileUpdates[0].ref.FilePath, err)
	}

	for i := range fileUpdates {
		data, err = updater.UpdateBytes(data, &fileUpdates[i].ref, fileUpdates[i].info.NewVersion)
		if err != nil {
			return nil, fmt.Errorf("updating chart %s in %s: %w", fileUpdates[i].info.ChartName, fileUpdates[i].ref.FilePath, err)
		}
	}

	return data, nil
}

// collectInfos extracts UpdateInfo from a slice of resolvedUpdates.
func collectInfos(updates []resolvedUpdate) []pr.UpdateInfo {
	infos := make([]pr.UpdateInfo, len(updates))
	for i := range updates {
		infos[i] = updates[i].info
	}
	return infos
}

// createPerFilePRs creates one PR per file, grouping all chart updates within that file.
func createPerFilePRs(ctx context.Context, settings *config.Settings, updates []resolvedUpdate, prCreator pr.Creator, maxPRCount int) int {
	groups, fileKeys := groupByFile(updates)
	prsCreated := 0

	for _, filePath := range fileKeys {
		if maxPRCount > 0 && prsCreated >= maxPRCount {
			slog.Info("reached max PR limit", "limit", maxPRCount)
			break
		}

		fileUpdates := groups[filePath]
		group := pr.UpdateGroup{
			Updates: collectInfos(fileUpdates),
			Files:   []pr.FileUpdate{{FilePath: filePath}},
		}

		// Check for existing PR
		branch, err := pr.RenderTemplate(settings.GroupBranchTemplate, pr.NewGroupTemplateData(group))
		if err != nil {
			slog.Error("failed to render group branch template", "file", filePath, "error", err)
			continue
		}

		exists, err := prCreator.ExistingPR(ctx, branch)
		if err != nil {
			slog.Warn("error checking existing PR", "file", filePath, "error", err)
		}
		if exists {
			slog.Info("PR already exists, skipping", "file", filePath, "branch", branch)
			continue
		}

		// Apply chained updates to the file
		updatedContent, err := applyFileUpdates(fileUpdates)
		if err != nil {
			slog.Error("failed to apply file updates", "file", filePath, "error", err)
			continue
		}

		group.Files[0].FileContent = updatedContent

		result, err := prCreator.CreateGroupPR(ctx, group, settings.BaseBranch)
		if err != nil {
			slog.Error("failed to create group PR", "file", filePath, "error", err)
			continue
		}

		fmt.Printf("Created PR: %s\n", result.PRURL)
		prsCreated++
	}

	return prsCreated
}

// createBatchPR creates a single PR for all chart updates across all files.
func createBatchPR(ctx context.Context, settings *config.Settings, updates []resolvedUpdate, prCreator pr.Creator) (int, error) {
	groups, fileKeys := groupByFile(updates)

	files := make([]pr.FileUpdate, len(fileKeys))
	for i, fp := range fileKeys {
		files[i] = pr.FileUpdate{FilePath: fp}
	}
	group := pr.UpdateGroup{
		Updates: collectInfos(updates),
		Files:   files,
	}

	// Check for existing PR
	branch, err := pr.RenderTemplate(settings.GroupBranchTemplate, pr.NewGroupTemplateData(group))
	if err != nil {
		return 0, fmt.Errorf("rendering group branch template: %w", err)
	}

	exists, err := prCreator.ExistingPR(ctx, branch)
	if err != nil {
		slog.Warn("error checking existing PR", "error", err)
	}
	if exists {
		slog.Info("batch PR already exists, skipping", "branch", branch)
		return 0, nil
	}

	// Apply chained updates per file
	for i, filePath := range fileKeys {
		updatedContent, err := applyFileUpdates(groups[filePath])
		if err != nil {
			return 0, fmt.Errorf("applying updates to %s: %w", filePath, err)
		}
		group.Files[i].FileContent = updatedContent
	}

	result, err := prCreator.CreateGroupPR(ctx, group, settings.BaseBranch)
	if err != nil {
		return 0, fmt.Errorf("creating batch PR: %w", err)
	}

	fmt.Printf("Created PR: %s\n", result.PRURL)
	return 1, nil
}

func resolveRepo(slug string) (owner, repo string, err error) {
	if slug == "" {
		slug = os.Getenv("GITHUB_REPOSITORY")
	}
	if slug == "" {
		return "", "", errors.New("repository not specified (use --repo or set GITHUB_REPOSITORY)")
	}

	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository format %q (expected owner/repo)", slug)
	}
	return parts[0], parts[1], nil
}
