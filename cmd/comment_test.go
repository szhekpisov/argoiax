package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
)

func writeEventFile(t *testing.T, event any) string {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setEventPath(t *testing.T, path string) {
	t.Helper()
	t.Setenv("GITHUB_EVENT_PATH", path)
}

func prCommentEvent(action, body string, isPR bool) map[string]any {
	event := map[string]any{
		"action": action,
		"issue": map[string]any{
			"number": 7,
		},
		"comment": map[string]any{
			"id":   42,
			"body": body,
		},
	}
	if isPR {
		issue := event["issue"].(map[string]any)
		issue["pull_request"] = map[string]any{
			"url": "https://api.github.com/repos/testowner/testrepo/pulls/7",
		}
	}
	return event
}

// DO NOT add t.Parallel — overrides package-level newGitHubClient and scanManifests.

func TestRunComment_MissingEventPath(t *testing.T) {
	t.Setenv("GITHUB_EVENT_PATH", "")
	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err == nil {
		t.Fatal("expected error for missing GITHUB_EVENT_PATH")
	}
}

func TestRunComment_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	setEventPath(t, path)

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRunComment_ActionNotCreated(t *testing.T) {
	event := prCommentEvent("edited", "@argoiax rebase", true)
	setEventPath(t, writeEventFile(t, event))

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("expected nil for non-created action, got %v", err)
	}
}

func TestRunComment_NotAPR(t *testing.T) {
	event := prCommentEvent("created", "@argoiax rebase", false)
	setEventPath(t, writeEventFile(t, event))

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("expected nil for non-PR comment, got %v", err)
	}
}

func TestRunComment_NoMention(t *testing.T) {
	event := prCommentEvent("created", "just a regular comment", true)
	setEventPath(t, writeEventFile(t, event))

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("expected nil for no mention, got %v", err)
	}
}

func TestRunComment_Rebase(t *testing.T) {
	event := prCommentEvent("created", "@argoiax rebase", true)
	setEventPath(t, writeEventFile(t, event))

	var reactionCreated, branchUpdated bool
	client := newMockGitHubAPIWithHandlers(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			reactionCreated = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"PUT /repos/testowner/testrepo/pulls/7/update-branch": func(w http.ResponseWriter, _ *http.Request) {
			branchUpdated = true
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"message":"Updating pull request branch."}`)
		},
	})
	overrideGitHubClient(t, client)

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reactionCreated {
		t.Error("expected reaction to be created")
	}
	if !branchUpdated {
		t.Error("expected branch to be updated")
	}
}

func TestRunComment_Recreate(t *testing.T) {
	event := prCommentEvent("created", "@argoiax recreate", true)
	setEventPath(t, writeEventFile(t, event))

	var prClosed, branchDeleted bool
	client := newMockGitHubAPIWithHandlers(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"GET /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, _ *http.Request) {
			pr := map[string]any{
				"number": 7,
				"state":  "open",
				"head":   map[string]any{"ref": "argoiax/mychart-1.2.0"},
			}
			_ = json.NewEncoder(w).Encode(pr)
		},
		"PATCH /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, _ *http.Request) {
			prClosed = true
			fmt.Fprint(w, `{"number":7,"state":"closed"}`)
		},
		"DELETE /repos/testowner/testrepo/git/refs/heads/argoiax/mychart-1.2.0": func(w http.ResponseWriter, _ *http.Request) {
			branchDeleted = true
			w.WriteHeader(http.StatusNoContent)
		},
	})
	overrideGitHubClient(t, client)

	// Override scanManifests to return no refs so runUpdate completes quickly
	overrideScanManifests(t, func(_ *config.Config, _, _ string) ([]manifest.ChartReference, error) {
		return nil, nil
	})

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prClosed {
		t.Error("expected PR to be closed")
	}
	if !branchDeleted {
		t.Error("expected branch to be deleted")
	}
}

func TestRunComment_UnknownCommand(t *testing.T) {
	event := prCommentEvent("created", "@argoiax deploy", true)
	setEventPath(t, writeEventFile(t, event))

	var reactionCreated, commentPosted bool
	client := newMockGitHubAPIWithHandlers(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			reactionCreated = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"confused"}`)
		},
		"POST /repos/testowner/testrepo/issues/7/comments": func(w http.ResponseWriter, _ *http.Request) {
			commentPosted = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":99}`)
		},
	})
	overrideGitHubClient(t, client)

	root := &rootOptions{}
	err := runComment(context.Background(), root, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reactionCreated {
		t.Error("expected confused reaction to be created")
	}
	if !commentPosted {
		t.Error("expected unknown command reply to be posted")
	}
}

