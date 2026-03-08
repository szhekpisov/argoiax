package comment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v68/github"
)

func newMockCommentAPI(t *testing.T, handlers map[string]http.HandlerFunc) *github.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := handlers[key]; ok {
			h(w, r)
			return
		}
		t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing mock server URL: %v", err)
	}
	client.BaseURL = baseURL
	return client
}

func TestRebase(t *testing.T) {
	var reactionCreated, branchUpdated bool

	client := newMockCommentAPI(t, map[string]http.HandlerFunc{
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

	client := newMockCommentAPI(t, map[string]http.HandlerFunc{
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

	headBranch, err := CloseAndDeleteBranch(context.Background(), ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if headBranch != "argoiax/mychart-1.2.0" {
		t.Errorf("headBranch = %q, want %q", headBranch, "argoiax/mychart-1.2.0")
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
	client := newMockCommentAPI(t, map[string]http.HandlerFunc{
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

func TestCloseAndDeleteBranch_GetPRError(t *testing.T) {
	client := newMockCommentAPI(t, map[string]http.HandlerFunc{
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
