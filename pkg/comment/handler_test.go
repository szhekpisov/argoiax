package comment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/szhekpisov/argoiax/internal/testutil"
)

func TestRebase(t *testing.T) {
	var reactionCreated, branchUpdated bool

	client := testutil.NewMockGitHubClient(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			reactionCreated = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"PUT /repos/testowner/testrepo/pulls/7/update-branch": func(w http.ResponseWriter, _ *http.Request) {
			branchUpdated = true
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"message":"Updating pull request branch.","url":"https://github.com/testowner/testrepo/pull/7"}`)
		},
	})

	ec := &EventContext{
		Client:    client,
		Owner:     "testowner",
		Repo:      "testrepo",
		PRNumber:  7,
		CommentID: 42,
	}

	if err := Rebase(context.Background(), ec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reactionCreated {
		t.Error("expected thumbs-up reaction to be created")
	}
	if !branchUpdated {
		t.Error("expected UpdateBranch to be called")
	}
}

func TestCloseAndDeleteBranch(t *testing.T) {
	var reactionCreated, prClosed, branchDeleted bool

	client := testutil.NewMockGitHubClient(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			reactionCreated = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"GET /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, _ *http.Request) {
			pr := map[string]any{
				"number": 7,
				"state":  "open",
				"head":   map[string]any{"ref": "argoiax/mychart-1.2.0"},
				"body":   "Bumps [mychart](https://charts.example.com) from 1.0.0 to 1.2.0.\n",
			}
			_ = json.NewEncoder(w).Encode(pr)
		},
		"PATCH /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["state"] != "closed" {
				t.Errorf("expected state=closed, got %q", body["state"])
			}
			prClosed = true
			fmt.Fprint(w, `{"number":7,"state":"closed"}`)
		},
		"DELETE /repos/testowner/testrepo/git/refs/heads/argoiax/mychart-1.2.0": func(w http.ResponseWriter, _ *http.Request) {
			branchDeleted = true
			w.WriteHeader(http.StatusNoContent)
		},
	})

	ec := &EventContext{
		Client:    client,
		Owner:     "testowner",
		Repo:      "testrepo",
		PRNumber:  7,
		CommentID: 42,
	}

	closed, err := CloseAndDeleteBranch(context.Background(), ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if closed.HeadBranch != "argoiax/mychart-1.2.0" {
		t.Errorf("HeadBranch = %q, want %q", closed.HeadBranch, "argoiax/mychart-1.2.0")
	}
	if closed.ChartName != "mychart" {
		t.Errorf("ChartName = %q, want %q", closed.ChartName, "mychart")
	}
	if !reactionCreated {
		t.Error("expected thumbs-up reaction to be created")
	}
	if !prClosed {
		t.Error("expected PR to be closed")
	}
	if !branchDeleted {
		t.Error("expected branch to be deleted")
	}
}

func TestRebase_UpdateBranchError(t *testing.T) {
	client := testutil.NewMockGitHubClient(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"PUT /repos/testowner/testrepo/pulls/7/update-branch": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"message":"merge conflict"}`)
		},
	})

	ec := &EventContext{
		Client:    client,
		Owner:     "testowner",
		Repo:      "testrepo",
		PRNumber:  7,
		CommentID: 42,
	}

	err := Rebase(context.Background(), ec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCloseAndDeleteBranch_DeleteBranchError(t *testing.T) {
	client := testutil.NewMockGitHubClient(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"GET /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, _ *http.Request) {
			pr := map[string]any{
				"number": 7,
				"state":  "open",
				"head":   map[string]any{"ref": "argoiax/mychart-1.2.0"},
				"body":   "Bumps [mychart](https://charts.example.com) from 1.0.0 to 1.2.0.\n",
			}
			_ = json.NewEncoder(w).Encode(pr)
		},
		"PATCH /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"number":7,"state":"closed"}`)
		},
		"DELETE /repos/testowner/testrepo/git/refs/heads/argoiax/mychart-1.2.0": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"message":"Reference does not exist"}`)
		},
	})

	ec := &EventContext{
		Client:    client,
		Owner:     "testowner",
		Repo:      "testrepo",
		PRNumber:  7,
		CommentID: 42,
	}

	closed, err := CloseAndDeleteBranch(context.Background(), ec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if closed.HeadBranch != "argoiax/mychart-1.2.0" {
		t.Errorf("HeadBranch = %q, want %q", closed.HeadBranch, "argoiax/mychart-1.2.0")
	}
	if !strings.Contains(err.Error(), "deleting branch") {
		t.Errorf("expected 'deleting branch' in error, got: %v", err)
	}
}

func TestCloseAndDeleteBranch_GetPRError(t *testing.T) {
	client := testutil.NewMockGitHubClient(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"+1"}`)
		},
		"GET /repos/testowner/testrepo/pulls/7": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		},
	})

	ec := &EventContext{
		Client:    client,
		Owner:     "testowner",
		Repo:      "testrepo",
		PRNumber:  7,
		CommentID: 42,
	}

	_, err := CloseAndDeleteBranch(context.Background(), ec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExtractChartName(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "per-chart PR body",
			body: "Bumps [mychart](https://charts.example.com) from 1.0.0 to 1.2.0.\n",
			want: "mychart",
		},
		{
			name: "group PR body",
			body: "## Updated Charts\n\n| Chart | File | Version |\n",
			want: "",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
		{
			name: "chart with dashes",
			body: "Bumps [cert-manager](https://charts.example.com) from 1.0.0 to 1.2.0.\n",
			want: "cert-manager",
		},
		{
			name: "structured marker",
			body: "<!-- argoiax:chart=mychart -->\nBumps [mychart](https://charts.example.com) from 1.0.0 to 1.2.0.\n",
			want: "mychart",
		},
		{
			name: "structured marker takes precedence",
			body: "<!-- argoiax:chart=real-chart -->\nBumps [wrong-chart](https://charts.example.com) from 1.0.0 to 1.2.0.\n",
			want: "real-chart",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractChartName(tt.body)
			if got != tt.want {
				t.Errorf("extractChartName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReplyError(t *testing.T) {
	var reactionCreated, commentPosted bool
	var postedBody string

	client := testutil.NewMockGitHubClient(t, map[string]http.HandlerFunc{
		"POST /repos/testowner/testrepo/issues/comments/42/reactions": func(w http.ResponseWriter, _ *http.Request) {
			reactionCreated = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"content":"-1"}`)
		},
		"POST /repos/testowner/testrepo/issues/7/comments": func(w http.ResponseWriter, r *http.Request) {
			commentPosted = true
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			postedBody = body["body"]
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":99}`)
		},
	})

	ec := &EventContext{
		Client:    client,
		Owner:     "testowner",
		Repo:      "testrepo",
		PRNumber:  7,
		CommentID: 42,
	}

	ReplyError(context.Background(), ec, "rebase", fmt.Errorf("merge conflict"))

	if !reactionCreated {
		t.Error("expected -1 reaction to be created")
	}
	if !commentPosted {
		t.Error("expected error comment to be posted")
	}
	if !strings.Contains(postedBody, "rebase") || !strings.Contains(postedBody, "workflow logs") {
		t.Errorf("unexpected comment body: %s", postedBody)
	}
	if strings.Contains(postedBody, "merge conflict") {
		t.Errorf("comment body should not contain raw error: %s", postedBody)
	}
}
