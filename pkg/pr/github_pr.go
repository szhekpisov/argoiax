package pr

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/vertrost/argoiax/pkg/config"
)

// GitHubCreator implements Creator using the GitHub API.
type GitHubCreator struct {
	client   *github.Client
	owner    string
	repo     string
	settings config.Settings
}

// NewGitHubCreator creates a new GitHubCreator.
func NewGitHubCreator(client *github.Client, owner, repo string, settings *config.Settings) *GitHubCreator {
	return &GitHubCreator{
		client:   client,
		owner:    owner,
		repo:     repo,
		settings: *settings,
	}
}

// CreatePR creates a pull request for a single chart update.
func (g *GitHubCreator) CreatePR(ctx context.Context, info *UpdateInfo, fileContent []byte, baseBranch string) (*Result, error) {
	branch, err := RenderTemplate(g.settings.BranchTemplate, info)
	if err != nil {
		return nil, fmt.Errorf("rendering branch template: %w", err)
	}

	if err := g.createBranch(ctx, branch, baseBranch); err != nil {
		return nil, err
	}

	commitMsg := fmt.Sprintf("chore(deps): update %s from %s to %s", info.ChartName, info.OldVersion, info.NewVersion)
	if err := g.commitFile(ctx, branch, info.FilePath, fileContent, commitMsg); err != nil {
		g.deleteBranch(ctx, branch)
		return nil, err
	}

	title, err := RenderTemplate(g.settings.TitleTemplate, info)
	if err != nil {
		title = fmt.Sprintf("chore(deps): update %s to %s", info.ChartName, info.NewVersion)
	}

	body := RenderPRBody(info)
	labels := g.buildLabels(info.IsBreaking)

	return g.submitPR(ctx, title, body, branch, baseBranch, labels)
}

// CreateGroupPR creates a pull request for a group of chart updates.
func (g *GitHubCreator) CreateGroupPR(ctx context.Context, group UpdateGroup, baseBranch string) (*Result, error) {
	data := NewGroupTemplateData(group)

	branch, err := RenderTemplate(g.settings.GroupBranchTemplate, data)
	if err != nil {
		return nil, fmt.Errorf("rendering group branch template: %w", err)
	}

	if err := g.createBranch(ctx, branch, baseBranch); err != nil {
		return nil, err
	}

	for _, file := range group.Files {
		commitMsg := "chore(deps): update charts in " + file.FilePath
		if err := g.commitFile(ctx, branch, file.FilePath, file.FileContent, commitMsg); err != nil {
			g.deleteBranch(ctx, branch)
			return nil, err
		}
	}

	title, err := RenderTemplate(g.settings.GroupTitleTemplate, data)
	if err != nil {
		title = fmt.Sprintf("chore(deps): update %d chart(s)", len(group.Updates))
	}

	body := RenderGroupPRBody(group)

	hasBreaking := slices.ContainsFunc(group.Updates, func(u UpdateInfo) bool { return u.IsBreaking })
	labels := g.buildLabels(hasBreaking)

	return g.submitPR(ctx, title, body, branch, baseBranch, labels)
}

// createBranch creates a new branch from the given base branch.
func (g *GitHubCreator) createBranch(ctx context.Context, branch, baseBranch string) error {
	baseRef, _, err := g.client.Git.GetRef(ctx, g.owner, g.repo, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("getting base branch ref: %w", err)
	}

	newRef := &github.Reference{
		Ref:    github.Ptr("refs/heads/" + branch),
		Object: baseRef.Object,
	}
	_, _, err = g.client.Git.CreateRef(ctx, g.owner, g.repo, newRef)
	if err != nil {
		return fmt.Errorf("creating branch %s: %w", branch, err)
	}
	return nil
}

// commitFile fetches the current file SHA and commits updated content to the branch.
func (g *GitHubCreator) commitFile(ctx context.Context, branch, filePath string, content []byte, message string) error {
	existingFile, _, _, err := g.client.Repositories.GetContents(ctx, g.owner, g.repo, filePath, &github.RepositoryContentGetOptions{Ref: branch})
	if err != nil {
		return fmt.Errorf("getting file %s: %w", filePath, err)
	}

	_, _, err = g.client.Repositories.UpdateFile(ctx, g.owner, g.repo, filePath, &github.RepositoryContentFileOptions{
		Message: &message,
		Content: content,
		SHA:     existingFile.SHA,
		Branch:  &branch,
		Author: &github.CommitAuthor{
			Name:  github.Ptr("argoiax"),
			Email: github.Ptr("argoiax[bot]@users.noreply.github.com"),
			Date:  &github.Timestamp{Time: time.Now()},
		},
	})
	if err != nil {
		return fmt.Errorf("updating file %s: %w", filePath, err)
	}
	return nil
}

// buildLabels returns a copy of the configured labels, adding "breaking-change" if needed.
func (g *GitHubCreator) buildLabels(isBreaking bool) []string {
	labels := make([]string, len(g.settings.Labels))
	copy(labels, g.settings.Labels)
	if isBreaking && !slices.Contains(labels, LabelBreakingChange) {
		labels = append(labels, LabelBreakingChange)
	}
	return labels
}

// submitPR creates the pull request, adds labels, and cleans up on failure.
func (g *GitHubCreator) submitPR(ctx context.Context, title, body, branch, baseBranch string, labels []string) (*Result, error) {
	pullRequest, _, err := g.client.PullRequests.Create(ctx, g.owner, g.repo, &github.NewPullRequest{
		Title: &title,
		Head:  &branch,
		Base:  &baseBranch,
		Body:  &body,
	})
	if err != nil {
		g.deleteBranch(ctx, branch)
		return nil, fmt.Errorf("creating PR: %w", err)
	}

	if len(labels) > 0 {
		_, _, err = g.client.Issues.AddLabelsToIssue(ctx, g.owner, g.repo, pullRequest.GetNumber(), labels)
		if err != nil {
			slog.Warn("failed to add labels", "error", err)
		}
	}

	return &Result{
		PRURL:    pullRequest.GetHTMLURL(),
		PRNumber: pullRequest.GetNumber(),
		Branch:   branch,
	}, nil
}

func (g *GitHubCreator) deleteBranch(ctx context.Context, branch string) {
	_, err := g.client.Git.DeleteRef(ctx, g.owner, g.repo, "refs/heads/"+branch)
	if err != nil {
		slog.Warn("failed to clean up branch", "branch", branch, "error", err)
	}
}

// ExistingPR checks if an open PR already exists for the given branch.
func (g *GitHubCreator) ExistingPR(ctx context.Context, branch string) (bool, error) {
	prs, _, err := g.client.PullRequests.List(ctx, g.owner, g.repo, &github.PullRequestListOptions{
		Head:  fmt.Sprintf("%s:%s", g.owner, branch),
		State: "open",
	})
	if err != nil {
		return false, err
	}
	return len(prs) > 0, nil
}
