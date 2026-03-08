package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/szhekpisov/argoiax/pkg/config"
)

func TestCreatePerChartPRs_Basic(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
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
	mock := &mockCreator{existing: map[string]int{"argoiax/mychart-1.1.0": 42}}
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
	mock := &mockCreator{existing: map[string]int{}}
	updates := []resolvedUpdate{
		makeUpdate(p1, "chart1", "1.0.0", "1.1.0"),
		makeUpdate(p2, "chart2", "2.0.0", "2.1.0"),
	}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 1)
	if count != 1 {
		t.Fatalf("expected 1 PR (max limit), got %d", count)
	}
}

func TestCreatePerChartPRs_BranchRenderError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	settings.BranchTemplate = "{{.InvalidField}}" // invalid template field
	mock := &mockCreator{existing: map[string]int{}}
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

func TestCreatePerChartPRs_ApplyFileError(t *testing.T) {
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
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
	// ExistingPR returns (0, error) when existingErr is set, so existingPR == 0 and we proceed to create
	mock := &errMockCreator{existingErr: errors.New("API error")}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)
	// Should still try to create the PR despite the ExistingPR error
	if count != 1 {
		t.Fatalf("expected 1 PR (warning on ExistingPR error), got %d", count)
	}
}

func TestCreatePerChartPRs_UpdatePRBodyError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{existingVal: 42, updateBodyErr: errors.New("permission denied")}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	count := createPerChartPRs(context.Background(), &settings, updates, mock, 10)
	// UpdatePRBody fails but processing should continue (error is logged, not fatal)
	if count != 0 {
		t.Fatalf("expected 0 PRs created (existing PR update attempted), got %d", count)
	}
	if mock.updateBodyCall != 1 {
		t.Fatalf("expected UpdatePRBody to be called once, got %d", mock.updateBodyCall)
	}
}

func TestCreatePerFilePRs_Basic(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
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

func TestCreatePerFilePRs_MaxPRLimit(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
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
	mock := &mockCreator{existing: map[string]int{"argoiax/update-app1": 10}}
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

func TestCreatePerFilePRs_BranchRenderError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	settings.GroupBranchTemplate = "{{.InvalidField}}"
	mock := &mockCreator{existing: map[string]int{}}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs (branch render error), got %d", count)
	}
}

func TestCreatePerFilePRs_ApplyFileError(t *testing.T) {
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
	updates := []resolvedUpdate{makeUpdate("/nonexistent/file.yaml", "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs (apply error), got %d", count)
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

func TestCreatePerFilePRs_UpdatePRBodyError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{existingVal: 10, updateBodyErr: errors.New("permission denied")}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count := createPerFilePRs(context.Background(), &settings, updates, mock, 10)
	if count != 0 {
		t.Fatalf("expected 0 PRs created (existing PR update attempted), got %d", count)
	}
	if mock.updateBodyCall != 1 {
		t.Fatalf("expected UpdatePRBody to be called once, got %d", mock.updateBodyCall)
	}
}

func TestCreateBatchPR_Basic(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	p2 := writeTestManifest(t, dir, "app2", "chart2", "2.0.0")
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
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

func TestCreateBatchPR_ExistingPR(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	// Batch uses GroupBranchTemplate with FileBaseName="batch" for multi-file
	mock := &mockCreator{existing: map[string]int{"argoiax/update-app1": 10}}
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
	mock := &mockCreator{existing: map[string]int{}}
	updates := []resolvedUpdate{makeUpdate(p1, "chart1", "1.0.0", "1.1.0")}

	_, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err == nil {
		t.Fatal("expected error from branch render, got nil")
	}
}

func TestCreateBatchPR_ApplyFileError(t *testing.T) {
	settings := testSettings()
	mock := &mockCreator{existing: map[string]int{}}
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

func TestCreateBatchPR_UpdatePRBodyError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app1", "chart1", "1.0.0")
	settings := testSettings()
	mock := &errMockCreator{existingVal: 10, updateBodyErr: errors.New("permission denied")}
	updates := []resolvedUpdate{makeUpdate(path, "chart1", "1.0.0", "1.1.0")}

	count, err := createBatchPR(context.Background(), &settings, updates, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 PRs created, got %d", count)
	}
	if mock.updateBodyCall != 1 {
		t.Fatalf("expected UpdatePRBody to be called once, got %d", mock.updateBodyCall)
	}
}

func TestDispatchPRs_DefaultStrategy(t *testing.T) {
	dir := t.TempDir()
	path := writeTestManifest(t, dir, "app", "mychart", "1.0.0")
	settings := testSettings()
	cfg := config.DefaultConfig()
	cfg.Settings = settings
	mock := &mockCreator{existing: map[string]int{}}
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
	mock := &mockCreator{existing: map[string]int{}}
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
	mock := &mockCreator{existing: map[string]int{}}
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
	mock := &mockCreator{existing: map[string]int{}}
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
	mock := &mockCreator{existing: map[string]int{"argoiax/mychart-1.1.0": 42}}
	updates := []resolvedUpdate{makeUpdate(path, "mychart", "1.0.0", "1.1.0")}

	err := dispatchPRs(context.Background(), cfg, updates, mock, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.prs) != 0 {
		t.Fatalf("expected 0 PRs (all existing), got %d", len(mock.prs))
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
	client := newMockGitHubAPIWithBranches(t, "master", []string{"master", "main"})
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
