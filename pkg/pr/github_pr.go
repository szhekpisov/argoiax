package pr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/vertrost/ancaeus/pkg/config"
)

// GitHubCreator implements Creator using the GitHub API.
type GitHubCreator struct {
	client   *github.Client
	owner    string
	repo     string
	settings config.Settings
}

// NewGitHubCreator creates a new GitHubCreator.
func NewGitHubCreator(client *github.Client, owner, repo string, settings config.Settings) *GitHubCreator {
	return &GitHubCreator{
		client:   client,
		owner:    owner,
		repo:     repo,
		settings: settings,
	}
}

func (g *GitHubCreator) CreatePR(ctx context.Context, info UpdateInfo, fileContent []byte, baseBranch string) (*PRResult, error) {
	branch, err := RenderTemplate(g.settings.BranchTemplate, info)
	if err != nil {
		return nil, fmt.Errorf("rendering branch template: %w", err)
	}

	// Check if a PR already exists
	exists, err := g.ExistingPR(ctx, branch)
	if err != nil {
		slog.Warn("error checking existing PR", "error", err)
	}
	if exists {
		slog.Info("PR already exists", "branch", branch)
		return nil, fmt.Errorf("PR already exists for branch %s", branch)
	}

	// Get the base branch ref
	baseRef, _, err := g.client.Git.GetRef(ctx, g.owner, g.repo, "refs/heads/"+baseBranch)
	if err != nil {
		return nil, fmt.Errorf("getting base branch ref: %w", err)
	}

	// Create the new branch
	newRef := &github.Reference{
		Ref:    github.Ptr("refs/heads/" + branch),
		Object: baseRef.Object,
	}
	_, _, err = g.client.Git.CreateRef(ctx, g.owner, g.repo, newRef)
	if err != nil {
		return nil, fmt.Errorf("creating branch %s: %w", branch, err)
	}

	// Get the current file to get its SHA
	existingFile, _, _, err := g.client.Repositories.GetContents(ctx, g.owner, g.repo, info.FilePath, &github.RepositoryContentGetOptions{Ref: branch})
	if err != nil {
		return nil, fmt.Errorf("getting file %s: %w", info.FilePath, err)
	}

	// Update the file
	commitMsg := fmt.Sprintf("chore(deps): update %s from %s to %s", info.ChartName, info.OldVersion, info.NewVersion)
	_, _, err = g.client.Repositories.UpdateFile(ctx, g.owner, g.repo, info.FilePath, &github.RepositoryContentFileOptions{
		Message: &commitMsg,
		Content: fileContent,
		SHA:     existingFile.SHA,
		Branch:  &branch,
		Author: &github.CommitAuthor{
			Name:  github.Ptr("ancaeus"),
			Email: github.Ptr("ancaeus[bot]@users.noreply.github.com"),
			Date:  &github.Timestamp{Time: time.Now()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("updating file: %w", err)
	}

	// Generate PR title and body
	title, err := RenderTemplate(g.settings.TitleTemplate, info)
	if err != nil {
		title = fmt.Sprintf("chore(deps): update %s to %s", info.ChartName, info.NewVersion)
	}

	body := RenderPRBody(info)

	// Create the PR
	labels := make([]string, len(g.settings.Labels))
	copy(labels, g.settings.Labels)
	if info.IsBreaking {
		hasLabel := false
		for _, l := range labels {
			if l == "breaking-change" {
				hasLabel = true
				break
			}
		}
		if !hasLabel {
			labels = append(labels, "breaking-change")
		}
	}

	pr, _, err := g.client.PullRequests.Create(ctx, g.owner, g.repo, &github.NewPullRequest{
		Title: &title,
		Head:  &branch,
		Base:  &baseBranch,
		Body:  &body,
	})
	if err != nil {
		return nil, fmt.Errorf("creating PR: %w", err)
	}

	// Add labels
	if len(labels) > 0 {
		_, _, err = g.client.Issues.AddLabelsToIssue(ctx, g.owner, g.repo, pr.GetNumber(), labels)
		if err != nil {
			slog.Warn("failed to add labels", "error", err)
		}
	}

	return &PRResult{
		PRURL:    pr.GetHTMLURL(),
		PRNumber: pr.GetNumber(),
		Branch:   branch,
	}, nil
}

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

// RenderTemplate executes a Go text/template against an UpdateInfo.
func RenderTemplate(tmpl string, info UpdateInfo) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := t.Execute(&buf, info); err != nil {
		return "", err
	}
	return buf.String(), nil
}
