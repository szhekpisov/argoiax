package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/szhekpisov/argoiax/pkg/config"
)

func newTestGitHubServer(t *testing.T) *github.Client {
	t.Helper()
	mux := http.NewServeMux()

	// GetRef — return a valid ref
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/main"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})

	// CreateRef — accept branch creation
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/test-branch"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})

	// GetContents — return file with SHA
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		content := &github.RepositoryContent{SHA: new("file-sha-123")}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(content)
	})

	// UpdateFile — accept file update
	mux.HandleFunc("PUT /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		resp := &github.RepositoryContentResponse{
			Content: &github.RepositoryContent{SHA: new("new-sha-456")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// CreatePullRequest / EditPullRequest — return PR
	prHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&github.PullRequest{
			Number:  new(42),
			HTMLURL: new("https://github.com/owner/repo/pull/42"),
		})
	}
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/pulls", prHandler)
	mux.HandleFunc("PATCH /api/v3/repos/{owner}/{repo}/pulls/{number}", prHandler)

	// AddLabelsToIssue — accept labels
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/issues/{issue}/labels", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]*github.Label{{Name: new("test")}})
	})

	// ListPullRequests
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/pulls", func(w http.ResponseWriter, r *http.Request) {
		head := r.URL.Query().Get("head")
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(head, "existing-branch") {
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{{Number: new(1)}})
		} else {
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{})
		}
	})

	// DeleteRef — accept branch deletion
	mux.HandleFunc("DELETE /api/v3/repos/{owner}/{repo}/git/refs/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)

	return client
}

func TestNewGitHubCreator(t *testing.T) {
	client := github.NewClient(nil)
	settings := &config.Settings{
		BaseBranch:     "main",
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		Labels:         []string{"dependencies"},
	}

	creator := NewGitHubCreator(client, "myowner", "myrepo", settings)

	if creator.owner != "myowner" {
		t.Errorf("expected owner myowner, got %s", creator.owner)
	}
	if creator.repo != "myrepo" {
		t.Errorf("expected repo myrepo, got %s", creator.repo)
	}
	if creator.settings.BaseBranch != "main" {
		t.Errorf("expected baseBranch main, got %s", creator.settings.BaseBranch)
	}
}

func TestBuildLabels(t *testing.T) {
	tests := []struct {
		name       string
		labels     []string
		isBreaking bool
		wantLen    int
		wantBreak  bool
	}{
		{"no labels not breaking", nil, false, 0, false},
		{"with labels not breaking", []string{"dep", "auto"}, false, 2, false},
		{"breaking adds label", []string{"dep"}, true, 2, true},
		{"breaking already has label", []string{"dep", LabelBreakingChange}, true, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GitHubCreator{settings: config.Settings{Labels: tt.labels}}
			got := g.buildLabels(tt.isBreaking)
			if len(got) != tt.wantLen {
				t.Errorf("expected %d labels, got %d: %v", tt.wantLen, len(got), got)
			}
			if tt.wantBreak {
				found := false
				for _, l := range got {
					if l == LabelBreakingChange {
						found = true
					}
				}
				if !found {
					t.Errorf("expected %q label in %v", LabelBreakingChange, got)
				}
			}
		})
	}
}

func TestBuildLabels_DoesNotMutateOriginal(t *testing.T) {
	original := []string{"dep"}
	g := &GitHubCreator{settings: config.Settings{Labels: original}}
	_ = g.buildLabels(true)
	if len(original) != 1 {
		t.Error("buildLabels mutated the original slice")
	}
}

func TestCreatePR(t *testing.T) {
	client := newTestGitHubServer(t)
	settings := &config.Settings{
		BaseBranch:     "main",
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}} to {{.NewVersion}}",
		Labels:         []string{"dependencies"},
	}
	creator := NewGitHubCreator(client, "owner", "repo", settings)

	info := &UpdateInfo{
		ChartName:  "mychart",
		OldVersion: "1.0.0",
		NewVersion: "1.1.0",
		FilePath:   "apps/app.yaml",
		RepoURL:    "https://charts.example.com",
	}

	result, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
	if result.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("expected PR URL, got %s", result.PRURL)
	}
}

func TestCreateGroupPR(t *testing.T) {
	client := newTestGitHubServer(t)
	settings := &config.Settings{
		BaseBranch:          "main",
		GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
		GroupTitleTemplate:  "update {{.Count}} chart(s)",
		Labels:              []string{"dependencies"},
	}
	creator := NewGitHubCreator(client, "owner", "repo", settings)

	group := UpdateGroup{
		Updates: []UpdateInfo{
			{ChartName: "chart1", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "apps/app.yaml"},
			{ChartName: "chart2", OldVersion: "2.0.0", NewVersion: "2.1.0", FilePath: "apps/app.yaml"},
		},
		Files: []FileUpdate{
			{FilePath: "apps/app.yaml", FileContent: []byte("content1")},
		},
	}

	result, err := creator.CreateGroupPR(context.Background(), group, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
}

func TestExistingPR(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{})

	prNum, err := creator.ExistingPR(context.Background(), "existing-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prNum == 0 {
		t.Error("expected existing PR to be found")
	}

	prNum, err = creator.ExistingPR(context.Background(), "new-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prNum != 0 {
		t.Error("expected no existing PR")
	}
}

func TestUpdatePRBody(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{})

	err := creator.UpdatePRBody(context.Background(), 42, "updated body text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdatePRBody_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/v3/repos/{owner}/{repo}/pulls/{number}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)

	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{})
	err := creator.UpdatePRBody(context.Background(), 42, "body")
	if err == nil {
		t.Fatal("expected error on 422 response")
	}
	if !strings.Contains(err.Error(), "updating PR #42") {
		t.Errorf("expected error to mention PR number, got: %v", err)
	}
}

func TestCreatePR_CommitFileError(t *testing.T) {
	mux := http.NewServeMux()
	// GetRef — return a valid ref
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/main"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// CreateRef — accept branch creation
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/test-branch"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// GetContents — return error
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})
	// DeleteRef — accept branch deletion (cleanup)
	mux.HandleFunc("DELETE /api/v3/repos/{owner}/{repo}/git/refs/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}}",
	})

	info := &UpdateInfo{ChartName: "chart", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}
	_, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err == nil {
		t.Error("expected error when GetContents fails")
	}
	if !strings.Contains(err.Error(), "getting file") {
		t.Errorf("expected 'getting file' in error, got: %v", err)
	}
}

func TestCreatePR_TitleRenderFallback(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BaseBranch:     "main",
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "{{.NonExistentField.Sub}}", // will fail at execute time
		Labels:         []string{"dependencies"},
	})

	info := &UpdateInfo{
		ChartName:  "mychart",
		OldVersion: "1.0.0",
		NewVersion: "1.1.0",
		FilePath:   "apps/app.yaml",
		RepoURL:    "https://charts.example.com",
	}

	result, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err != nil {
		t.Fatalf("expected fallback title to prevent error, got: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
}

func TestCreateGroupPR_CommitFileError(t *testing.T) {
	mux := http.NewServeMux()
	// GetRef — return a valid ref
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/main"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// CreateRef — accept branch creation
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/test-branch"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// GetContents — return file with SHA (for commitFile to proceed)
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		content := &github.RepositoryContent{SHA: new("file-sha-123")}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(content)
	})
	// UpdateFile — return error
	mux.HandleFunc("PUT /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})
	// DeleteRef — accept branch deletion (cleanup)
	mux.HandleFunc("DELETE /api/v3/repos/{owner}/{repo}/git/refs/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
		GroupTitleTemplate:  "update {{.Count}} chart(s)",
		Labels:              []string{"dependencies"},
	})

	group := UpdateGroup{
		Updates: []UpdateInfo{
			{ChartName: "chart1", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "apps/app.yaml"},
		},
		Files: []FileUpdate{
			{FilePath: "apps/app.yaml", FileContent: []byte("content1")},
		},
	}

	_, err := creator.CreateGroupPR(context.Background(), group, "main")
	if err == nil {
		t.Error("expected error when UpdateFile fails")
	}
	if !strings.Contains(err.Error(), "updating file") {
		t.Errorf("expected 'updating file' in error, got: %v", err)
	}
}

func TestCreateGroupPR_TitleRenderFallback(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BaseBranch:          "main",
		GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
		GroupTitleTemplate:  "{{.NonExistentField.Sub}}", // will fail at execute time
		Labels:              []string{"dependencies"},
	})

	group := UpdateGroup{
		Updates: []UpdateInfo{
			{ChartName: "chart1", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "apps/app.yaml"},
		},
		Files: []FileUpdate{
			{FilePath: "apps/app.yaml", FileContent: []byte("content1")},
		},
	}

	result, err := creator.CreateGroupPR(context.Background(), group, "main")
	if err != nil {
		t.Fatalf("expected fallback title to prevent error, got: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
}

func TestCreatePR_SubmitPRError(t *testing.T) {
	mux := http.NewServeMux()
	// GetRef — return a valid ref
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/main"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// CreateRef — accept branch creation
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/test-branch"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// GetContents — return file with SHA
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		content := &github.RepositoryContent{SHA: new("file-sha-123")}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(content)
	})
	// UpdateFile — accept file update
	mux.HandleFunc("PUT /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		resp := &github.RepositoryContentResponse{
			Content: &github.RepositoryContent{SHA: new("new-sha-456")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// CreatePullRequest — return error
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/pulls", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Validation Failed"}`)
	})
	// DeleteRef — accept branch deletion (cleanup after PR creation failure)
	mux.HandleFunc("DELETE /api/v3/repos/{owner}/{repo}/git/refs/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}}",
	})

	info := &UpdateInfo{ChartName: "chart", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}
	_, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err == nil {
		t.Error("expected error when PR creation fails")
	}
	if !strings.Contains(err.Error(), "creating PR") {
		t.Errorf("expected 'creating PR' in error, got: %v", err)
	}
}

func TestExistingPR_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/pulls", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{})

	_, err := creator.ExistingPR(context.Background(), "some-branch")
	if err == nil {
		t.Error("expected error when PR list API fails")
	}
}

func TestCreatePR_LabelFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/main"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/test-branch"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		content := &github.RepositoryContent{SHA: new("file-sha-123")}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(content)
	})
	mux.HandleFunc("PUT /api/v3/repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		resp := &github.RepositoryContentResponse{
			Content: &github.RepositoryContent{SHA: new("new-sha-456")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/pulls", func(w http.ResponseWriter, _ *http.Request) {
		pr := &github.PullRequest{
			Number:  new(42),
			HTMLURL: new("https://github.com/owner/repo/pull/42"),
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pr)
	})
	// Labels API fails
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/issues/{issue}/labels", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}}",
		Labels:         []string{"dependencies"},
	})

	info := &UpdateInfo{ChartName: "chart", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}
	result, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	// Label failure is a warning, not an error
	if err != nil {
		t.Fatalf("expected no error (label failure is warning), got: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
}

func TestDeleteBranch_Failure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v3/repos/{owner}/{repo}/git/refs/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{})

	// Should not panic — just logs a warning
	creator.deleteBranch(context.Background(), "test-branch")
}

func TestCreatePR_NoLabels(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BaseBranch:     "main",
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}} to {{.NewVersion}}",
		Labels:         nil, // no labels
	})

	info := &UpdateInfo{
		ChartName:  "mychart",
		OldVersion: "1.0.0",
		NewVersion: "1.1.0",
		FilePath:   "apps/app.yaml",
		RepoURL:    "https://charts.example.com",
	}

	result, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
}

func TestCreatePR_BranchRenderError(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BranchTemplate: "{{.NonExistentField.Sub}}", // fails at execute time
		TitleTemplate:  "update {{.ChartName}}",
	})

	info := &UpdateInfo{ChartName: "chart", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}
	_, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err == nil {
		t.Error("expected error when branch template render fails")
	}
	if !strings.Contains(err.Error(), "rendering branch template") {
		t.Errorf("expected 'rendering branch template' in error, got: %v", err)
	}
}

func TestCreateGroupPR_BranchRenderError(t *testing.T) {
	client := newTestGitHubServer(t)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		GroupBranchTemplate: "{{.NonExistentField.Sub}}", // fails at execute time
		GroupTitleTemplate:  "update charts",
	})

	group := UpdateGroup{
		Updates: []UpdateInfo{{ChartName: "chart1", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}},
		Files:   []FileUpdate{{FilePath: "app.yaml", FileContent: []byte("content")}},
	}

	_, err := creator.CreateGroupPR(context.Background(), group, "main")
	if err == nil {
		t.Error("expected error when group branch template render fails")
	}
	if !strings.Contains(err.Error(), "rendering group branch template") {
		t.Errorf("expected 'rendering group branch template' in error, got: %v", err)
	}
}

func TestCreateGroupPR_CreateBranchError(t *testing.T) {
	mux := http.NewServeMux()
	// GetRef fails
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
		GroupTitleTemplate:  "update charts",
	})

	group := UpdateGroup{
		Updates: []UpdateInfo{{ChartName: "chart1", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}},
		Files:   []FileUpdate{{FilePath: "app.yaml", FileContent: []byte("content")}},
	}

	_, err := creator.CreateGroupPR(context.Background(), group, "main")
	if err == nil {
		t.Error("expected error when createBranch fails in CreateGroupPR")
	}
	if !strings.Contains(err.Error(), "getting base branch ref") {
		t.Errorf("expected 'getting base branch ref' in error, got: %v", err)
	}
}

func TestCreatePR_BranchCreationFailure(t *testing.T) {
	mux := http.NewServeMux()
	// GetRef fails
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}}",
	})

	info := &UpdateInfo{ChartName: "chart", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}
	_, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err == nil {
		t.Error("expected error when branch creation fails")
	}
}

func TestCreateBranch_CreateRefFailure(t *testing.T) {
	mux := http.NewServeMux()
	// GetRef succeeds
	mux.HandleFunc("GET /api/v3/repos/{owner}/{repo}/git/ref/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		ref := &github.Reference{
			Ref:    new("refs/heads/main"),
			Object: &github.GitObject{SHA: new("abc123")},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ref)
	})
	// CreateRef fails
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repo}/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"message":"Reference already exists"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	client, _ = client.WithEnterpriseURLs(srv.URL, srv.URL)
	creator := NewGitHubCreator(client, "owner", "repo", &config.Settings{
		BranchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:  "update {{.ChartName}}",
	})

	info := &UpdateInfo{ChartName: "chart", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "app.yaml"}
	_, err := creator.CreatePR(context.Background(), info, []byte("content"), "main")
	if err == nil {
		t.Error("expected error when CreateRef fails")
	}
	if !strings.Contains(err.Error(), "creating branch") {
		t.Errorf("expected 'creating branch' in error, got: %v", err)
	}
}
