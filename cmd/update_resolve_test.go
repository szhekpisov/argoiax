package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
	"github.com/szhekpisov/argoiax/pkg/registry"
	"github.com/szhekpisov/argoiax/pkg/releasenotes"
)

func TestResolveUpdates_Basic(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	cfg.Release.Enabled = false
	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")
	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.0.0", Type: manifest.SourceTypeHTTP},
	}

	updates := resolveUpdates(context.Background(), cfg, refs, factory, orch, false)

	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].info.NewVersion != "1.2.0" {
		t.Errorf("expected new version 1.2.0, got %s", updates[0].info.NewVersion)
	}
	if updates[0].info.OldVersion != "1.0.0" {
		t.Errorf("expected old version 1.0.0, got %s", updates[0].info.OldVersion)
	}
}

func TestResolveUpdates_SkipsMajor(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	cfg.Release.Enabled = false
	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")
	refs := []manifest.ChartReference{
		{ChartName: "otherchart", RepoURL: srv.URL, TargetRevision: "2.0.0", Type: manifest.SourceTypeHTTP},
	}

	updates := resolveUpdates(context.Background(), cfg, refs, factory, orch, false)
	if len(updates) != 0 {
		t.Fatalf("expected 0 updates (major skipped), got %d", len(updates))
	}

	updates = resolveUpdates(context.Background(), cfg, refs, factory, orch, true)
	if len(updates) != 1 {
		t.Fatalf("expected 1 update (major allowed), got %d", len(updates))
	}
}

func TestResolveUpdates_AlreadyCurrent(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	cfg.Release.Enabled = false
	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")
	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.2.0", Type: manifest.SourceTypeHTTP},
	}

	updates := resolveUpdates(context.Background(), cfg, refs, factory, orch, false)
	if len(updates) != 0 {
		t.Fatalf("expected 0 updates (already current), got %d", len(updates))
	}
}

func TestResolveUpdates_CancelledContext(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	cfg.Release.Enabled = false
	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.0.0", Type: manifest.SourceTypeHTTP},
	}

	updates := resolveUpdates(ctx, cfg, refs, factory, orch, false)

	// With a cancelled context, the semaphore acquire should fail and return no updates
	if len(updates) != 0 {
		t.Errorf("expected 0 updates with cancelled context, got %d", len(updates))
	}
}

func TestResolveUpdates_MultipleSorted(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	cfg.Release.Enabled = false
	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")
	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.0.0", Type: manifest.SourceTypeHTTP, FilePath: "b.yaml"},
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.1.0", Type: manifest.SourceTypeHTTP, FilePath: "a.yaml"},
		{ChartName: "otherchart", RepoURL: srv.URL, TargetRevision: "2.0.0", Type: manifest.SourceTypeHTTP, FilePath: "c.yaml"},
	}

	updates := resolveUpdates(context.Background(), cfg, refs, factory, orch, true)

	if len(updates) != 3 {
		t.Fatalf("expected 3 updates, got %d", len(updates))
	}
	// Should be sorted by chart name first, then file path
	if updates[0].info.ChartName != "mychart" || updates[0].info.FilePath != "a.yaml" {
		t.Errorf("expected first update mychart/a.yaml, got %s/%s", updates[0].info.ChartName, updates[0].info.FilePath)
	}
	if updates[2].info.ChartName != "otherchart" {
		t.Errorf("expected last update otherchart, got %s", updates[2].info.ChartName)
	}
}

func TestResolveOneUpdate_ResolveError(t *testing.T) {
	// Use a server that returns no chart, causing resolveLatest to fail
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			_, _ = w.Write([]byte("entries: {}"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	cfg := config.DefaultConfig()
	cfg.Release.Enabled = false
	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")

	ref := &manifest.ChartReference{
		ChartName:      "nonexistent",
		RepoURL:        server.URL,
		TargetRevision: "1.0.0",
		Type:           manifest.SourceTypeHTTP,
	}

	_, ok := resolveOneUpdate(context.Background(), cfg, ref, factory, orch, false)
	if ok {
		t.Error("expected resolveOneUpdate to fail for nonexistent chart")
	}
}

func TestResolveOneUpdate_WithIntermediateVersions(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	cfg.Release.Enabled = true
	cfg.Release.IncludeIntermediate = true
	cfg.Release.Sources = nil // no sources, so notes will be nil but the path is exercised

	factory := registry.NewFactory(cfg, "")
	orch := releasenotes.NewOrchestrator(cfg.Release, "")

	ref := &manifest.ChartReference{
		ChartName:      "mychart",
		RepoURL:        srv.URL,
		TargetRevision: "1.0.0",
		Type:           manifest.SourceTypeHTTP,
	}

	u, ok := resolveOneUpdate(context.Background(), cfg, ref, factory, orch, false)
	if !ok {
		t.Fatal("expected resolveOneUpdate to succeed")
	}
	if u.info.NewVersion != "1.2.0" {
		t.Errorf("expected new version 1.2.0, got %s", u.info.NewVersion)
	}
	if u.info.OldVersion != "1.0.0" {
		t.Errorf("expected old version 1.0.0, got %s", u.info.OldVersion)
	}
}

func TestResolveCredentials(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		slug      string
		dryRun    bool
		envToken  string
		wantToken string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"token from flag", "flag-token", "owner/repo", false, "", "flag-token", "owner", "repo", false},
		{"token from env", "", "owner/repo", false, "env-token", "env-token", "owner", "repo", false},
		{"missing token not dry-run", "", "owner/repo", false, "", "", "", "", true},
		{"missing token dry-run", "", "owner/repo", true, "", "", "owner", "repo", false},
		{"invalid slug not dry-run", "token", "invalid", false, "", "", "", "", true},
		{"invalid slug dry-run", "token", "invalid", true, "", "token", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", tt.envToken)
			t.Setenv("GH_TOKEN", "")
			t.Setenv("GITHUB_REPOSITORY", "")

			token, owner, repo, err := resolveCredentials(tt.token, tt.slug, tt.dryRun)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestResolveRepo_EnvFallback(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "env-owner/env-repo")
	owner, repo, err := resolveRepo("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "env-owner" || repo != "env-repo" {
		t.Errorf("expected env-owner/env-repo, got %s/%s", owner, repo)
	}
}

func TestResolveRepo(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	tests := []struct {
		slug, wantOwner, wantRepo string
		wantErr                   bool
	}{
		{"owner/repo", "owner", "repo", false},
		{"my-org/my-repo", "my-org", "my-repo", false},
		{"invalid", "", "", true},
		{"", "", "", true},
	}
	for _, tt := range tests {
		owner, repo, err := resolveRepo(tt.slug)
		if (err != nil) != tt.wantErr {
			t.Errorf("resolveRepo(%q) error = %v, wantErr %v", tt.slug, err, tt.wantErr)
			continue
		}
		if owner != tt.wantOwner || repo != tt.wantRepo {
			t.Errorf("resolveRepo(%q) = (%q, %q), want (%q, %q)", tt.slug, owner, repo, tt.wantOwner, tt.wantRepo)
		}
	}
}

func TestResolveBaseBranch_AutoDetect(t *testing.T) {
	client := newMockGitHubAPI(t, "develop")
	cfg := config.DefaultConfig()

	err := resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.BaseBranch != "develop" {
		t.Errorf("expected baseBranch develop, got %s", cfg.Settings.BaseBranch)
	}
}

func TestResolveBaseBranch_ExplicitBranch(t *testing.T) {
	client := newMockGitHubAPIWithBranches(t, "main", []string{"main", "custom"})
	cfg := config.DefaultConfig()
	cfg.Settings.BaseBranch = "custom"

	err := resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.BaseBranch != "custom" {
		t.Errorf("expected baseBranch custom, got %s", cfg.Settings.BaseBranch)
	}
}

func TestResolveBaseBranch_EmptyDefaultBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"default_branch": ""}`)
	}))
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing mock server URL: %v", err)
	}
	client.BaseURL = baseURL

	cfg := config.DefaultConfig()
	err = resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err == nil {
		t.Fatal("expected error for empty default branch")
	}
}

func TestResolveBaseBranch_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing mock server URL: %v", err)
	}
	client.BaseURL = baseURL

	cfg := config.DefaultConfig()
	err = resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

func TestResolveBaseBranch_ConfiguredBranchNotFound(t *testing.T) {
	t.Parallel()
	// Repo default is "master", but config says "main" which doesn't exist.
	client := newMockGitHubAPIWithBranches(t, "master", []string{"master"})
	cfg := config.DefaultConfig()
	cfg.Settings.BaseBranch = "main"

	err := resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err == nil {
		t.Fatal("expected error for non-existent configured baseBranch")
	}
	if !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "contents permission") {
		t.Errorf("expected error to mention branch does not exist and contents permission, got: %v", err)
	}
}

func TestResolveBaseBranch_ConfiguredBranch_TransientError(t *testing.T) {
	t.Parallel()
	// GetRef returns 500 for the configured branch — should propagate as error, not fall back.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/repos/testowner/testrepo/git/ref/heads/") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, _ := url.Parse(server.URL + "/")
	client.BaseURL = baseURL

	cfg := config.DefaultConfig()
	cfg.Settings.BaseBranch = "main"

	err := resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err == nil {
		t.Fatal("expected error for transient API failure")
	}
	if !strings.Contains(err.Error(), "checking configured baseBranch") {
		t.Errorf("expected error to mention checking configured baseBranch, got: %v", err)
	}
}

func TestResolveBaseBranch_AutoDetect_NoContentsPermission(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/testowner/testrepo" && r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, `{"default_branch": "main"}`)
		case strings.HasPrefix(r.URL.Path, "/repos/testowner/testrepo/git/ref/heads/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, _ := url.Parse(server.URL + "/")
	client.BaseURL = baseURL

	cfg := config.DefaultConfig()

	err := resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err == nil {
		t.Fatal("expected error when token lacks contents:read permission")
	}
	if !strings.Contains(err.Error(), "not accessible") || !strings.Contains(err.Error(), "contents:read") {
		t.Errorf("expected error to mention inaccessibility and contents:read permission, got: %v", err)
	}
}

func TestResolveBaseBranch_AutoDetect_TransientError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/testowner/testrepo" && r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, `{"default_branch": "main"}`)
		case strings.HasPrefix(r.URL.Path, "/repos/testowner/testrepo/git/ref/heads/"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"message":"Internal Server Error"}`)
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, _ := url.Parse(server.URL + "/")
	client.BaseURL = baseURL

	cfg := config.DefaultConfig()

	err := resolveBaseBranch(context.Background(), client, "testowner", "testrepo", cfg)
	if err == nil {
		t.Fatal("expected error on transient API failure")
	}
	if !strings.Contains(err.Error(), "checking default branch") {
		t.Errorf("expected error to mention checking default branch, got: %v", err)
	}
}
