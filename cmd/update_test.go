package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/manifest"
	"github.com/vertrost/argoiax/pkg/pr"
	"github.com/vertrost/argoiax/pkg/registry"
	"github.com/vertrost/argoiax/pkg/releasenotes"
)

type mockCreator struct {
	existing map[string]bool
	prs      []*pr.UpdateInfo
	groupPRs []pr.UpdateGroup
}

func (m *mockCreator) ExistingPR(_ context.Context, branch string) (bool, error) {
	return m.existing[branch], nil
}

func (m *mockCreator) CreatePR(_ context.Context, info *pr.UpdateInfo, _ []byte, _ string) (*pr.Result, error) {
	m.prs = append(m.prs, info)
	n := len(m.prs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func (m *mockCreator) CreateGroupPR(_ context.Context, group pr.UpdateGroup, _ string) (*pr.Result, error) {
	m.groupPRs = append(m.groupPRs, group)
	n := len(m.groupPRs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func newTestHelmServer(t *testing.T) *httptest.Server {
	t.Helper()
	idx := `entries:
  mychart:
    - version: "1.0.0"
    - version: "1.1.0"
    - version: "1.2.0"
  otherchart:
    - version: "2.0.0"
    - version: "3.0.0"
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			_, _ = fmt.Fprint(w, idx)
		} else {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeTestManifest(t *testing.T, dir, name, chart, version string) string {
	t.Helper()
	content := fmt.Sprintf("apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: %s\nspec:\n  source:\n    repoURL: https://charts.example.com\n    chart: %s\n    targetRevision: %s\n", name, chart, version)
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeUpdate(filePath, chartName, oldVer, newVer string) resolvedUpdate {
	return resolvedUpdate{
		ref: manifest.ChartReference{
			ChartName: chartName, RepoURL: "https://charts.example.com",
			TargetRevision: oldVer, Type: manifest.SourceTypeHTTP,
			FilePath: filePath, YAMLPath: "spec.source.targetRevision", SourceIndex: -1,
		},
		info: pr.UpdateInfo{
			ChartName: chartName, OldVersion: oldVer, NewVersion: newVer,
			FilePath: filePath, RepoURL: "https://charts.example.com",
		},
	}
}

func testSettings() config.Settings {
	return config.Settings{
		BaseBranch:          "main",
		BranchTemplate:      "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:       "update {{.ChartName}} to {{.NewVersion}}",
		GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
		GroupTitleTemplate:  "update {{.Count}} charts",
		MaxOpenPRs:          10,
	}
}

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

func TestCreatePerChartPRs_Basic(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)

	if count != 1 {
		t.Fatalf("expected 1 PR, got %d", count)
	}
	if mock.prs[0].ChartName != "mychart" {
		t.Errorf("expected chart mychart, got %s", mock.prs[0].ChartName)
	}
}

func TestCreatePerChartPRs_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{"argoiax/mychart-1.1.0": true}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs (existing), got %d", count)
	}
}

func TestCreatePerChartPRs_MaxPRLimit(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{
		makeUpdate(p1, "chart1", "1.0.0", "1.1.0"),
		makeUpdate(p2, "chart2", "2.0.0", "2.1.0"),
	}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 1)
	if count != 1 {
		t.Fatalf("expected 1 PR (max limit), got %d", count)
	}
}

func TestCreatePerFilePRs_Basic(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{
		makeUpdate(p1, "chart1", "1.0.0", "1.1.0"),
		makeUpdate(p2, "chart2", "2.0.0", "2.1.0"),
	}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	if count != 2 {
		t.Fatalf("expected 2 PRs, got %d", count)
	}
	if len(mock.groupPRs) != 2 {
		t.Fatalf("expected 2 group PRs, got %d", len(mock.groupPRs))
	}
}

func TestCreateBatchPR_Basic(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{
		makeUpdate(p1, "chart1", "1.0.0", "1.1.0"),
		makeUpdate(p2, "chart2", "2.0.0", "2.1.0"),
	}

	count, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 PR (batch), got %d", count)
	}
	if len(mock.groupPRs[0].Updates) != 2 {
		t.Errorf("expected 2 updates in batch, got %d", len(mock.groupPRs[0].Updates))
	}
}

func TestGroupByFile(t *testing.T) {
	updates := []resolvedUpdate{
		{info: pr.UpdateInfo{FilePath: "a.yaml", ChartName: "chart1"}},
		{info: pr.UpdateInfo{FilePath: "b.yaml", ChartName: "chart2"}},
		{info: pr.UpdateInfo{FilePath: "a.yaml", ChartName: "chart3"}},
	}

	groups, keys := groupByFile(updates)

	if len(keys) != 2 || keys[0] != "a.yaml" || keys[1] != "b.yaml" {
		t.Errorf("expected keys [a.yaml, b.yaml], got %v", keys)
	}
	if len(groups["a.yaml"]) != 2 {
		t.Errorf("expected 2 updates for a.yaml, got %d", len(groups["a.yaml"]))
	}
	if len(groups["b.yaml"]) != 1 {
		t.Errorf("expected 1 update for b.yaml, got %d", len(groups["b.yaml"]))
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

func TestPrintDryRun(t *testing.T) {
	updates := []resolvedUpdate{
		{info: pr.UpdateInfo{ChartName: "chart1", FilePath: "app.yaml", OldVersion: "1.0.0", NewVersion: "1.1.0"}},
		{info: pr.UpdateInfo{ChartName: "chart2", FilePath: "app2.yaml", OldVersion: "2.0.0", NewVersion: "3.0.0", IsBreaking: true}},
	}

	// Should not panic — output goes to stdout
	printDryRun(updates)
}

func TestCollectInfos(t *testing.T) {
	updates := []resolvedUpdate{
		{info: pr.UpdateInfo{ChartName: "a", NewVersion: "1.0.0"}},
		{info: pr.UpdateInfo{ChartName: "b", NewVersion: "2.0.0"}},
		{info: pr.UpdateInfo{ChartName: "c", NewVersion: "3.0.0"}},
	}
	infos := collectInfos(updates)
	if len(infos) != 3 {
		t.Fatalf("expected 3 infos, got %d", len(infos))
	}
	if infos[0].ChartName != "a" || infos[1].ChartName != "b" || infos[2].ChartName != "c" {
		t.Errorf("unexpected chart names: %v", infos)
	}
}

func TestApplyFileUpdates_Empty(t *testing.T) {
	_, err := applyFileUpdates(nil)
	if err == nil {
		t.Error("expected error for empty updates")
	}
}

func TestApplyFileUpdates_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")

	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}
	data, err := applyFileUpdates(updates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
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
