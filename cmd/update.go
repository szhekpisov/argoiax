package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
	"github.com/szhekpisov/argoiax/pkg/output"
	"github.com/szhekpisov/argoiax/pkg/pr"
	"github.com/szhekpisov/argoiax/pkg/registry"
	"github.com/szhekpisov/argoiax/pkg/releasenotes"
	"github.com/szhekpisov/argoiax/pkg/semver"
	"github.com/szhekpisov/argoiax/pkg/updater"
	"golang.org/x/oauth2"
	"golang.org/x/sync/semaphore"
)

func newUpdateCmd(root *rootOptions) *cobra.Command {
	var (
		chartFilter string
		allowMajor  bool
		maxPRs      int
		githubToken string
		repoSlug    string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Create PRs for outdated Helm chart versions",
		Long:  `Update scans for outdated charts, modifies YAML files, and creates pull requests on GitHub.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd.Context(), root, chartFilter, allowMajor, maxPRs, githubToken, repoSlug)
		},
	}

	cmd.Flags().StringVar(&chartFilter, "chart", "", "only update a specific chart name")
	cmd.Flags().BoolVar(&allowMajor, "allow-major", false, "include major version updates")
	cmd.Flags().IntVar(&maxPRs, "max-prs", 0, "maximum number of PRs to create (0 = use config)")
	cmd.Flags().StringVar(&githubToken, "github-token", "", "GitHub token (or set GITHUB_TOKEN env var)")
	cmd.Flags().StringVar(&repoSlug, "repo", "", "GitHub repository (owner/repo)")

	return cmd
}

// resolvedUpdate holds a resolved chart update with all metadata needed for PR creation.
type resolvedUpdate struct {
	ref  manifest.ChartReference
	info pr.UpdateInfo
}

func runUpdate(ctx context.Context, root *rootOptions, chartFilter string, allowMajor bool, maxPRs int, githubToken, repoSlug string) error {
	cfg, err := config.Load(root.cfgFile)
	if err != nil {
		return err
	}

	token, owner, repo, err := resolveCredentials(githubToken, repoSlug, root.dryRun)
	if err != nil {
		return err
	}

	refs, err := scanManifests(cfg, root.scanDir, chartFilter)
	if err != nil {
		return err
	}

	updates := resolveUpdates(ctx, cfg, refs,
		registry.NewFactory(cfg, token),
		releasenotes.NewOrchestrator(cfg.Release, token),
		allowMajor)

	if len(updates) == 0 {
		if !root.dryRun {
			fmt.Println("No updates to create PRs for.")
		}
		return nil
	}

	if root.dryRun {
		printDryRun(updates)
		return nil
	}

	return createPRs(ctx, cfg, token, owner, repo, updates, maxPRs)
}

func resolveCredentials(githubToken, repoSlug string, dryRun bool) (token, owner, repo string, err error) {
	token = githubToken
	if token == "" {
		token = registry.GetGitHubToken()
	}
	if token == "" && !dryRun {
		return "", "", "", errors.New("GitHub token required (use --github-token or set GITHUB_TOKEN)")
	}

	owner, repo, err = resolveRepo(repoSlug)
	if err != nil && !dryRun {
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

// Package-level function variables to allow overriding in tests.
// Tests that override these must NOT use t.Parallel().
var (
	newGitHubClient = defaultNewGitHubClient
	scanManifests   = scanRefs
)

func defaultNewGitHubClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	tc.Timeout = 60 * time.Second
	tc.Transport = &registry.RetryTransport{Base: tc.Transport, MaxRetries: 3}
	return github.NewClient(tc)
}

func createPRs(ctx context.Context, cfg *config.Config, token, owner, repo string, updates []resolvedUpdate, maxPRs int) error {
	ghClient := newGitHubClient(ctx, token)
	if err := resolveBaseBranch(ctx, ghClient, owner, repo, cfg); err != nil {
		return err
	}
	prCreator := pr.NewGitHubCreator(ghClient, owner, repo, &cfg.Settings)
	return dispatchPRs(ctx, cfg, updates, prCreator, maxPRs)
}

func branchExists(ctx context.Context, ghClient *github.Client, owner, repo, branch string) (bool, error) {
	_, _, err := ghClient.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

func resolveBaseBranch(ctx context.Context, ghClient *github.Client, owner, repo string, cfg *config.Config) error {
	if cfg.Settings.BaseBranch != "" {
		exists, err := branchExists(ctx, ghClient, owner, repo, cfg.Settings.BaseBranch)
		if err != nil {
			return fmt.Errorf("checking configured baseBranch %q: %w", cfg.Settings.BaseBranch, err)
		}
		if !exists {
			return fmt.Errorf("configured baseBranch %q does not exist in %s/%s; ensure the token has contents permission", cfg.Settings.BaseBranch, owner, repo)
		}
		return nil
	}

	ghRepo, _, err := ghClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("getting repository default branch: %w", err)
	}
	branch := ghRepo.GetDefaultBranch()
	if branch == "" {
		return fmt.Errorf("repository %s/%s has no default branch", owner, repo)
	}

	cfg.Settings.BaseBranch = branch
	slog.Info("detected default branch", "branch", cfg.Settings.BaseBranch)
	return nil
}

func dispatchPRs(ctx context.Context, cfg *config.Config, updates []resolvedUpdate, prCreator pr.Creator, maxPRs int) error {
	maxPRCount := maxPRs
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

// resolveUpdates resolves version updates for all refs concurrently, fetches release notes, and detects breaking changes.
func resolveUpdates(ctx context.Context, cfg *config.Config, refs []manifest.ChartReference, factory registry.FactoryInterface, notesOrch *releasenotes.Orchestrator, allowMajor bool) []resolvedUpdate {
	const maxConcurrency = 10
	sem := semaphore.NewWeighted(maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var updates []resolvedUpdate

	for i := range refs {
		wg.Add(1)
		go func(ref *manifest.ChartReference) {
			defer wg.Done()
			if err := sem.Acquire(ctx, 1); err != nil {
				slog.Error("failed to acquire semaphore", "chart", ref.ChartName, "error", err)
				return
			}
			defer sem.Release(1)

			u, ok := resolveOneUpdate(ctx, cfg, ref, factory, notesOrch, allowMajor)
			if !ok {
				return
			}
			mu.Lock()
			updates = append(updates, u)
			mu.Unlock()
		}(&refs[i])
	}

	wg.Wait()

	slices.SortFunc(updates, func(a, b resolvedUpdate) int {
		if c := strings.Compare(a.info.ChartName, b.info.ChartName); c != 0 {
			return c
		}
		return strings.Compare(a.info.FilePath, b.info.FilePath)
	})

	return updates
}

func resolveOneUpdate(ctx context.Context, cfg *config.Config, ref *manifest.ChartReference, factory registry.FactoryInterface, notesOrch *releasenotes.Orchestrator, allowMajor bool) (resolvedUpdate, bool) {
	latest, allVersions, chartCfg, err := resolveLatest(ctx, factory, cfg, ref)
	if err != nil {
		slog.Error("failed to resolve latest version", "chart", ref.ChartName, "error", err)
		return resolvedUpdate{}, false
	}

	if semver.Equal(latest, ref.TargetRevision) {
		return resolvedUpdate{}, false
	}

	isMajor := semver.IsMajorBump(ref.TargetRevision, latest)
	if isMajor && !allowMajor {
		slog.Info("skipping major update", "chart", ref.ChartName, "current", ref.TargetRevision, "latest", latest)
		return resolvedUpdate{}, false
	}

	slog.Info("update available", "chart", ref.ChartName, "current", ref.TargetRevision, "latest", latest)

	var versionsToFetch []string
	if cfg.Release.IncludeIntermediate {
		versionsToFetch = semver.VersionsBetween(allVersions, ref.TargetRevision, latest)
	}
	versionsToFetch = append(versionsToFetch, latest)
	notes := notesOrch.FetchNotes(ctx, ref.ChartName, ref.RepoURL, versionsToFetch, chartCfg)

	breakingResult := semver.DetectBreaking(ref.TargetRevision, latest, notes.CombinedBody())

	return resolvedUpdate{
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
	}, true
}

// createPerChartPRs creates one PR per chart update.
func createPerChartPRs(ctx context.Context, settings *config.Settings, updates []resolvedUpdate, prCreator pr.Creator, maxPRCount int) int {
	prsCreated := 0

	for i := range updates {
		if maxPRCount > 0 && prsCreated >= maxPRCount {
			slog.Info("reached max PR limit", "limit", maxPRCount)
			break
		}

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
