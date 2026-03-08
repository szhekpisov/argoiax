package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
	"github.com/szhekpisov/argoiax/pkg/pr"
	"github.com/szhekpisov/argoiax/pkg/registry"
	"github.com/szhekpisov/argoiax/pkg/releasenotes"
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

func TestApplyFileUpdates_UpdateError(t *testing.T) {
	dir := t.TempDir()
	// Write a manifest with version 1.0.0
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	// Create an update that references a version not in the file (mismatch)
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "9.9.9", "1.1.0")}
	_, err := applyFileUpdates(updates)
	if err == nil {
		t.Error("expected error for version mismatch in applyFileUpdates")
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

// errMockCreator is a mock Creator that returns errors from its methods.
type errMockCreator struct {
	existingErr error
	existingVal bool
	createPRErr error
	groupPRErr  error
	prs         []*pr.UpdateInfo
	groupPRs    []pr.UpdateGroup
}

func (m *errMockCreator) ExistingPR(_ context.Context, _ string) (bool, error) {
	if m.existingErr != nil {
		return false, m.existingErr
	}
	return m.existingVal, nil
}

func (m *errMockCreator) CreatePR(_ context.Context, info *pr.UpdateInfo, _ []byte, _ string) (*pr.Result, error) {
	if m.createPRErr != nil {
		return nil, m.createPRErr
	}
	m.prs = append(m.prs, info)
	n := len(m.prs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func (m *errMockCreator) CreateGroupPR(_ context.Context, group pr.UpdateGroup, _ string) (*pr.Result, error) {
	if m.groupPRErr != nil {
		return nil, m.groupPRErr
	}
	m.groupPRs = append(m.groupPRs, group)
	n := len(m.groupPRs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func TestCreatePerChartPRs_BranchRenderError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	settings.BranchTemplate = "{{.InvalidField}}" // invalid template field
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)

	if count != 0 {
		t.Fatalf("expected 0 PRs (branch render error), got %d", count)
	}
	if len(mock.prs) != 0 {
		t.Errorf("expected no PRs created, got %d", len(mock.prs))
	}
}

func TestCreatePerChartPRs_CreatePRError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{createPRErr: errors.New("API error")}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)

	if count != 0 {
		t.Fatalf("expected 0 PRs (create error), got %d", count)
	}
}

func TestCreatePerFilePRs_MaxPRLimit(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{
		makeUpdate(p1, "chart1", "1.0.0", "1.1.0"),
		makeUpdate(p2, "chart2", "2.0.0", "2.1.0"),
	}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 1)

	if count != 1 {
		t.Fatalf("expected 1 PR (max limit), got %d", count)
	}
}

func TestCreatePerFilePRs_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	// The group branch template is "argoiax/update-{{.FileBaseName}}" and FileBaseName is "app1"
	mock := &mockCreator{existing: map[string]bool{"argoiax/update-app1": true}}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)

	if count != 0 {
		t.Fatalf("expected 0 PRs (existing), got %d", count)
	}
}

func TestCreatePerFilePRs_CreateGroupPRError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{groupPRErr: errors.New("group PR API error")}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)

	if count != 0 {
		t.Fatalf("expected 0 PRs (group PR error), got %d", count)
	}
}

func TestCreateBatchPR_ExistingPR(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	// Batch uses GroupBranchTemplate with FileBaseName="batch" for multi-file
	mock := &mockCreator{existing: map[string]bool{"argoiax/update-app1": true}}
	updates := []resolvedUpdate{makeUpdate(p1, "chart1", "1.0.0", "1.1.0")}

	count, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 PRs (existing), got %d", count)
	}
}

func TestCreateBatchPR_BranchRenderError(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	settings.GroupBranchTemplate = "{{.InvalidField}}" // invalid template field
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(p1, "chart1", "1.0.0", "1.1.0")}

	_, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err == nil {
		t.Fatal("expected error from branch render, got nil")
	}
}

func TestApplyFileUpdates_FileNotFound(t *testing.T) {
	updates := []resolvedUpdate{makeUpdate("/nonexistent/path/file.yaml", "mychart", "1.0.0", "1.1.0")}

	_, err := applyFileUpdates(updates)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
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

func TestRunUpdate_DryRun(t *testing.T) {
	srv := newTestHelmServer(t)
	dir := t.TempDir()

	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.0.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	root := &rootOptions{scanDir: dir, dryRun: true}
	err := runUpdate(context.Background(), root, "", false, 0, "", "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error in dry-run: %v", err)
	}
}

func TestRunUpdate_DryRun_NoUpdates(t *testing.T) {
	srv := newTestHelmServer(t)
	dir := t.TempDir()

	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.2.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	root := &rootOptions{scanDir: dir, dryRun: true}
	err := runUpdate(context.Background(), root, "", false, 0, "", "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUpdate_NoUpdatesNonDryRun(t *testing.T) {
	srv := newTestHelmServer(t)
	dir := t.TempDir()

	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.2.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GITHUB_TOKEN", "fake-token")
	root := &rootOptions{scanDir: dir}
	err := runUpdate(context.Background(), root, "", false, 0, "fake-token", "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewUpdateCmd(t *testing.T) {
	root := &rootOptions{}
	cmd := newUpdateCmd(root)

	if cmd.Use != "update" {
		t.Errorf("expected Use update, got %s", cmd.Use)
	}

	for _, flag := range []string{"chart", "allow-major", "max-prs", "github-token", "repo"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected flag %q to be registered", flag)
		}
	}
}

func TestNewUpdateCmd_RunE(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	root := &rootOptions{scanDir: t.TempDir()}
	cmd := newUpdateCmd(root)
	cmd.SetArgs([]string{"--repo", "owner/repo"})
	// Should fail due to missing token
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestRunUpdate_MissingToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	root := &rootOptions{scanDir: t.TempDir()}
	err := runUpdate(context.Background(), root, "", false, 0, "", "owner/repo")
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestCreatePerFilePRs_BranchRenderError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	settings.GroupBranchTemplate = "{{.InvalidField}}"
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs (branch render error), got %d", count)
	}
}

func TestCreatePerFilePRs_ApplyFileError(t *testing.T) {
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate("/nonexistent/file.yaml", "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs (apply error), got %d", count)
	}
}

func TestCreatePerChartPRs_ApplyFileError(t *testing.T) {
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate("/nonexistent/file.yaml", "chart1", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs (apply error), got %d", count)
	}
}

func TestCreatePerChartPRs_ExistingPRCheckError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{existingErr: errors.New("API error"), existingVal: true}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	// ExistingPR returns error but existingVal is false (default when err), so it continues
	mock2 := &errMockCreator{existingErr: errors.New("API error")}
	count := createPerChartPRs(context.Background(), &settings, updates, mock2, 10)
	_ = mock
	// Should still try to create the PR despite the ExistingPR error
	if count != 1 {
		t.Fatalf("expected 1 PR (warning on ExistingPR error), got %d", count)
	}
}

func TestCreatePerFilePRs_ExistingPRCheckError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{existingErr: errors.New("API error")}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	// Should still try to create PR despite ExistingPR error
	if count != 1 {
		t.Fatalf("expected 1 PR (warning on ExistingPR error), got %d", count)
	}
}

func TestCreateBatchPR_ApplyFileError(t *testing.T) {
	settings := testSettings()
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate("/nonexistent/file.yaml", "chart1", "1.0.0", "1.1.0")}

	_, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err == nil {
		t.Fatal("expected error from apply file updates")
	}
}

func TestCreateBatchPR_ExistingPRCheckError(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{existingErr: errors.New("API error")}
	updates := []resolvedUpdate{makeUpdate(p1, "chart1", "1.0.0", "1.1.0")}

	count, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still try to create despite ExistingPR error
	if count != 1 {
		t.Fatalf("expected 1 PR (warning on ExistingPR error), got %d", count)
	}
}

func TestCreateBatchPR_CreateGroupPRError(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{groupPRErr: errors.New("group PR API error")}
	updates := []resolvedUpdate{makeUpdate(p1, "chart1", "1.0.0", "1.1.0")}

	_, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err == nil {
		t.Fatal("expected error from createGroupPR")
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

func TestRunUpdate_ConfigError(t *testing.T) {
	root := &rootOptions{scanDir: t.TempDir(), cfgFile: "/nonexistent/config.yaml"}
	err := runUpdate(context.Background(), root, "", false, 0, "token", "owner/repo")
	if err == nil {
		t.Error("expected error for non-existent config file")
	}
}

func TestRunUpdate_ScanRefsError(t *testing.T) {
	// Create a config that will result in a walkDir error by using a file as a dir
	dir := t.TempDir()
	cfgContent := "scanDirs: []\n"
	cfgPath := filepath.Join(dir, "argoiax.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}
	// scanDir empty, config has no dirs either — should succeed with 0 refs
	root := &rootOptions{scanDir: dir, cfgFile: cfgPath, dryRun: true}
	err := runUpdate(context.Background(), root, "", false, 0, "", "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatchPRs_DefaultStrategy(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.prs) != 1 {
		t.Fatalf("expected 1 PR created, got %d", len(mock.prs))
	}
}

func TestDispatchPRs_PerFileStrategy(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	settings.PRStrategy = config.StrategyPerFile
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.groupPRs) != 1 {
		t.Fatalf("expected 1 group PR created, got %d", len(mock.groupPRs))
	}
}

func TestDispatchPRs_BatchStrategy(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	settings.PRStrategy = config.StrategyBatch
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.groupPRs) != 1 {
		t.Fatalf("expected 1 batch PR created, got %d", len(mock.groupPRs))
	}
}

func TestDispatchPRs_BatchError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	settings.PRStrategy = config.StrategyBatch
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &errMockCreator{groupPRErr: errors.New("batch API error")}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 10)
	if err == nil {
		t.Fatal("expected error from batch strategy")
	}
}

func TestDispatchPRs_MaxPRsDefaultsToConfig(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	settings.MaxOpenPRs = 1
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &mockCreator{existing: map[string]bool{}}
	updates := []resolvedUpdate{
		makeUpdate(p1, "chart1", "1.0.0", "1.1.0"),
		makeUpdate(p2, "chart2", "2.0.0", "2.1.0"),
	}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.prs) != 1 {
		t.Fatalf("expected 1 PR (config MaxOpenPRs=1), got %d", len(mock.prs))
	}
}

func TestDispatchPRs_NoPRsCreated(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &mockCreator{existing: map[string]bool{"argoiax/mychart-1.1.0": true}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.prs) != 0 {
		t.Fatalf("expected 0 PRs (all existing), got %d", len(mock.prs))
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

// newMockGitHubAPI creates a test server that mocks the GitHub API endpoints
// needed for default branch detection and PR existence checks.
// ExistingPR always returns true (existing PR found), so the test avoids
// needing to mock the full PR creation flow (branch, commit, PR endpoints).
func newMockGitHubAPI(t *testing.T, defaultBranch string) *github.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/testowner/testrepo" && r.Method == http.MethodGet:
			_, _ = fmt.Fprintf(w, `{"default_branch": %q}`, defaultBranch)
		case r.URL.Path == "/repos/testowner/testrepo/pulls" && r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, `[{"number": 1, "html_url": "https://github.com/testowner/testrepo/pull/1"}]`)
		default:
			t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
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

func overrideGitHubClient(t *testing.T, client *github.Client) {
	t.Helper()
	orig := newGitHubClient
	t.Cleanup(func() { newGitHubClient = orig })
	newGitHubClient = func(_ context.Context, _ string) *github.Client {
		return client
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
	cfg := config.DefaultConfig()
	cfg.Settings.BaseBranch = "custom"

	// Should return immediately without making any API calls.
	err := resolveBaseBranch(context.Background(), nil, "testowner", "testrepo", cfg)
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

// DO NOT add t.Parallel — overrides package-level newGitHubClient.
func TestCreatePRs_DetectsDefaultBranch(t *testing.T) {
	client := newMockGitHubAPI(t, "master")
	overrideGitHubClient(t, client)

	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	cfg := config.DefaultConfig()
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := createPRs(context.Background(), cfg, "fake-token", "testowner", "testrepo", updates, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.BaseBranch != "master" {
		t.Errorf("expected baseBranch master, got %s", cfg.Settings.BaseBranch)
	}
}

// DO NOT add t.Parallel — overrides package-level newGitHubClient.
func TestCreatePRs_ExplicitBaseBranch(t *testing.T) {
	client := newMockGitHubAPI(t, "master")
	overrideGitHubClient(t, client)

	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	cfg := config.DefaultConfig()
	cfg.Settings.BaseBranch = "main"
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := createPRs(context.Background(), cfg, "fake-token", "testowner", "testrepo", updates, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.BaseBranch != "main" {
		t.Errorf("expected baseBranch to remain main, got %s", cfg.Settings.BaseBranch)
	}
}

// DO NOT add t.Parallel — overrides package-level newGitHubClient.
func TestCreatePRs_APIError(t *testing.T) {
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
	overrideGitHubClient(t, client)

	cfg := config.DefaultConfig()
	err = createPRs(context.Background(), cfg, "fake-token", "testowner", "testrepo", nil, 10)
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

// DO NOT add t.Parallel — overrides package-level newGitHubClient.
func TestRunUpdate_WithUpdates(t *testing.T) {
	srv := newTestHelmServer(t)
	client := newMockGitHubAPI(t, "main")
	overrideGitHubClient(t, client)

	dir := t.TempDir()
	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.0.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	root := &rootOptions{scanDir: dir}
	err := runUpdate(context.Background(), root, "", false, 0, "fake-token", "testowner/testrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// DO NOT add t.Parallel — overrides package-level scanManifests.
func TestRunUpdate_ScanError(t *testing.T) {
	orig := scanManifests
	t.Cleanup(func() { scanManifests = orig })
	scanManifests = func(_ *config.Config, _, _ string) ([]manifest.ChartReference, error) {
		return nil, errors.New("scan failed")
	}

	root := &rootOptions{scanDir: t.TempDir()}
	err := runUpdate(context.Background(), root, "", false, 0, "fake-token", "owner/repo")
	if err == nil {
		t.Fatal("expected error from scanManifests")
	}
}

func TestDefaultNewGitHubClient(t *testing.T) {
	client := defaultNewGitHubClient(context.Background(), "fake-token")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
