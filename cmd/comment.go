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
		var b strings.Builder
		fmt.Fprintf(&b, "Unknown command `%s`. Supported commands:\n", cmd.Name)
		for _, s := range supported {
			fmt.Fprintf(&b, "- `@argoiax %s`\n", s)
		}
		body := b.String()
		_, _, err := ghClient.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
			Body: &body,
		})
		if err != nil {
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
			return fmt.Errorf("rebase: %w", err)
		}
		slog.Info("rebased PR", "pr", prNumber)

	case "recreate":
		slog.Info("handling recreate command", "pr", prNumber)
		headBranch, err := comment.CloseAndDeleteBranch(ctx, ec)
		if err != nil {
			return fmt.Errorf("recreate (close): %w", err)
		}
		slog.Info("closed PR and deleted branch", "pr", prNumber, "branch", headBranch)

		// Determine chart and allow-major flags for the update re-run.
		// Pass empty chart filter so the update command re-scans everything;
		// its built-in ExistingPR dedup ensures only the missing PR is recreated.
		chartFilter := ""
		allowMajor := false

		// Parse flags from environment if present (forwarded by action.yml)
		if v := os.Getenv("INPUT_ALLOW_MAJOR"); strings.EqualFold(v, "true") {
			allowMajor = true
		}
		maxPRs := 0

		if err := runUpdate(ctx, root, chartFilter, allowMajor, maxPRs, githubToken, repoSlug); err != nil {
			return fmt.Errorf("recreate (update): %w", err)
		}
		slog.Info("recreated PR", "pr", prNumber)
	}

	return nil
}
