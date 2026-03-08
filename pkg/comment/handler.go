package comment

import (
	"context"
	"errors"
	"fmt"
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

// CloseAndDeleteBranch closes the PR and deletes its head branch.
// Returns the head branch name for logging.
func CloseAndDeleteBranch(ctx context.Context, ec *EventContext) (string, error) {
	if err := addReaction(ctx, ec, "+1"); err != nil {
		return "", fmt.Errorf("adding reaction: %w", err)
	}

	prObj, _, err := ec.Client.PullRequests.Get(ctx, ec.Owner, ec.Repo, ec.PRNumber)
	if err != nil {
		return "", fmt.Errorf("getting PR: %w", err)
	}

	headBranch := prObj.GetHead().GetRef()

	closed := "closed"
	_, _, err = ec.Client.PullRequests.Edit(ctx, ec.Owner, ec.Repo, ec.PRNumber, &github.PullRequest{
		State: &closed,
	})
	if err != nil {
		return headBranch, fmt.Errorf("closing PR: %w", err)
	}

	_, err = ec.Client.Git.DeleteRef(ctx, ec.Owner, ec.Repo, "heads/"+headBranch)
	if err != nil {
		return headBranch, fmt.Errorf("deleting branch %s: %w", headBranch, err)
	}

	return headBranch, nil
}

// ReplyUnknownCommand adds a "confused" reaction and posts a comment listing
// the supported commands.
func ReplyUnknownCommand(ctx context.Context, ec *EventContext, cmdName string) error {
	_ = addReaction(ctx, ec, "confused")

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

func addReaction(ctx context.Context, ec *EventContext, reaction string) error {
	_, _, err := ec.Client.Reactions.CreateIssueCommentReaction(ctx, ec.Owner, ec.Repo, ec.CommentID, reaction)
	return err
}
