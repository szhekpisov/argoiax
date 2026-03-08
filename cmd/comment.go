package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"github.com/szhekpisov/argoiax/pkg/comment"
)

func newCommentCmd(root *rootOptions) *cobra.Command {
	var (
		githubToken string
		repoSlug    string
	)

	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Handle PR comment commands",
		Long:  `Process @argoiax commands from PR comments (rebase, recreate).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runComment(cmd.Context(), root, githubToken, repoSlug)
		},
	}

	cmd.Flags().StringVar(&githubToken, "github-token", "", "GitHub token (or set GITHUB_TOKEN env var)")
	cmd.Flags().StringVar(&repoSlug, "repo", "", "GitHub repository (owner/repo)")

	return cmd
}

func runComment(ctx context.Context, root *rootOptions, githubToken, repoSlug string) error {
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return fmt.Errorf("GITHUB_EVENT_PATH not set")
	}

	data, err := os.ReadFile(eventPath)
	if err != nil {
		return fmt.Errorf("reading event file: %w", err)
	}

	var event github.IssueCommentEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("parsing event JSON: %w", err)
	}

	// Only handle newly created comments
	if event.GetAction() != "created" {
		return nil
	}

	// Only handle PR comments (not regular issue comments)
	if event.Issue == nil || event.Issue.PullRequestLinks == nil {
		return nil
	}

	cmd := comment.Parse(event.Comment.GetBody())
	if cmd == nil {
		return nil
	}

	token, owner, repo, err := resolveCredentials(githubToken, repoSlug, false)
	if err != nil {
		return err
	}

	ghClient := newGitHubClient(ctx, token)
	prNumber := event.Issue.GetNumber()

	// Validate command
	supported := comment.SupportedCommands()
	if !slices.Contains(supported, cmd.Name) {
		ec := &comment.EventContext{
			Client: ghClient, Owner: owner, Repo: repo,
			PRNumber: prNumber, CommentID: event.Comment.GetID(),
		}
		if err := comment.ReplyUnknownCommand(ctx, ec, cmd.Name); err != nil {
			return fmt.Errorf("posting unknown command reply: %w", err)
		}
		return nil
	}

	ec := &comment.EventContext{
		Client:    ghClient,
		Owner:     owner,
		Repo:      repo,
		PRNumber:  prNumber,
		CommentID: event.Comment.GetID(),
	}

	switch cmd.Name {
	case "rebase":
		slog.Info("handling rebase command", "pr", prNumber)
		if err := comment.Rebase(ctx, ec); err != nil {
			comment.ReplyError(ctx, ec, cmd.Name, err)
			return fmt.Errorf("rebase: %w", err)
		}
		slog.Info("rebased PR", "pr", prNumber)

	case "recreate":
		slog.Info("handling recreate command", "pr", prNumber)
		closed, err := comment.CloseAndDeleteBranch(ctx, ec)
		if err != nil {
			comment.ReplyError(ctx, ec, cmd.Name, err)
			return fmt.Errorf("recreate (close): %w", err)
		}
		slog.Info("closed PR and deleted branch", "pr", prNumber, "branch", closed.HeadBranch)

		// Use the chart name from the closed PR body to scope the recreate.
		// INPUT_CHART env var takes precedence if set.
		chartFilter := closed.ChartName
		if cf := os.Getenv("INPUT_CHART"); cf != "" {
			chartFilter = cf
		}
		allowMajor := strings.EqualFold(os.Getenv("INPUT_ALLOW_MAJOR"), "true")
		maxPRs := 1

		if err := runUpdate(ctx, root, chartFilter, allowMajor, maxPRs, githubToken, repoSlug); err != nil {
			comment.ReplyError(ctx, ec, cmd.Name, err)
			return fmt.Errorf("recreate (update): %w", err)
		}
		slog.Info("recreated PR", "pr", prNumber)
	}

	return nil
}
