package comment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/go-github/v68/github"
)

// EventContext holds the GitHub context needed for handling comment commands.
type EventContext struct {
	Client    *github.Client
	Owner     string
	Repo      string
	PRNumber  int
	CommentID int64
}

// ClosedPR holds the result of closing a PR and deleting its branch.
type ClosedPR struct {
	HeadBranch string
	ChartName  string // extracted from PR body; empty for group PRs
}

// Rebase updates the PR branch with the latest base branch using merge-based update.
func Rebase(ctx context.Context, ec *EventContext) error {
	if err := addReaction(ctx, ec, "+1"); err != nil {
		return fmt.Errorf("adding reaction: %w", err)
	}

	_, _, err := ec.Client.PullRequests.UpdateBranch(ctx, ec.Owner, ec.Repo, ec.PRNumber, nil)
	if err != nil {
		// UpdateBranch returns 202 Accepted, which go-github surfaces as AcceptedError.
		// This is expected and means the update was successfully scheduled.
		var acceptedErr *github.AcceptedError
		if errors.As(err, &acceptedErr) {
			return nil
		}
		return fmt.Errorf("updating PR branch: %w", err)
	}
	return nil
}

var (
	chartMarkerRe = regexp.MustCompile(`<!-- argoiax:chart=(\S+) -->`)
	chartNameRe   = regexp.MustCompile(`^\s*Bumps \[([^\]]+)\]`)
)

// CloseAndDeleteBranch closes the PR and deletes its head branch.
func CloseAndDeleteBranch(ctx context.Context, ec *EventContext) (*ClosedPR, error) {
	if err := addReaction(ctx, ec, "+1"); err != nil {
		return nil, fmt.Errorf("adding reaction: %w", err)
	}

	prObj, _, err := ec.Client.PullRequests.Get(ctx, ec.Owner, ec.Repo, ec.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("getting PR: %w", err)
	}

	result := &ClosedPR{
		HeadBranch: prObj.GetHead().GetRef(),
		ChartName:  extractChartName(prObj.GetBody()),
	}

	closed := "closed"
	_, _, err = ec.Client.PullRequests.Edit(ctx, ec.Owner, ec.Repo, ec.PRNumber, &github.PullRequest{
		State: &closed,
	})
	if err != nil {
		return result, fmt.Errorf("closing PR: %w", err)
	}

	_, err = ec.Client.Git.DeleteRef(ctx, ec.Owner, ec.Repo, "heads/"+result.HeadBranch)
	if err != nil {
		return result, fmt.Errorf("deleting branch %s: %w", result.HeadBranch, err)
	}

	return result, nil
}

// extractChartName parses the chart name from a per-chart PR body.
// Prefers the structured <!-- argoiax:chart=NAME --> marker, falling back
// to the "Bumps [NAME]" line. Returns empty string for group PRs.
func extractChartName(body string) string {
	if m := chartMarkerRe.FindStringSubmatch(body); len(m) >= 2 {
		return m[1]
	}
	if m := chartNameRe.FindStringSubmatch(body); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// ReplyUnknownCommand adds a "confused" reaction and posts a comment listing
// the supported commands.
func ReplyUnknownCommand(ctx context.Context, ec *EventContext, cmdName string) error {
	if err := addReaction(ctx, ec, "confused"); err != nil {
		slog.Warn("failed to add confused reaction", "error", err)
	}

	supported := SupportedCommands()
	var b strings.Builder
	fmt.Fprintf(&b, "Unknown command `%s`. Supported commands:\n", cmdName)
	for _, s := range supported {
		fmt.Fprintf(&b, "- `@argoiax %s`\n", s)
	}
	body := b.String()
	_, _, err := ec.Client.Issues.CreateComment(ctx, ec.Owner, ec.Repo, ec.PRNumber, &github.IssueComment{
		Body: &body,
	})
	if err != nil {
		return fmt.Errorf("posting reply: %w", err)
	}
	return nil
}

// ReplyError adds a "-1" reaction and posts an error comment on the PR.
func ReplyError(ctx context.Context, ec *EventContext, cmdName string, cmdErr error) {
	if err := addReaction(ctx, ec, "-1"); err != nil {
		slog.Warn("failed to add error reaction", "error", err)
	}

	slog.Error("command failed", "command", cmdName, "error", cmdErr)

	body := fmt.Sprintf("The `%s` command failed. Check the workflow logs for details.", cmdName)
	_, _, err := ec.Client.Issues.CreateComment(ctx, ec.Owner, ec.Repo, ec.PRNumber, &github.IssueComment{
		Body: &body,
	})
	if err != nil {
		slog.Warn("failed to post error reply", "error", err)
	}
}

func addReaction(ctx context.Context, ec *EventContext, reaction string) error {
	_, _, err := ec.Client.Reactions.CreateIssueCommentReaction(ctx, ec.Owner, ec.Repo, ec.CommentID, reaction)
	return err
}
