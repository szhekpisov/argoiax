package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/manifest"
	"github.com/vertrost/argoiax/pkg/output"
	"github.com/vertrost/argoiax/pkg/registry"
)

func TestNewScanCmd(t *testing.T) {
	root := &rootOptions{}
	cmd := newScanCmd(root)

	if cmd.Use != "scan" {
		t.Errorf("expected Use scan, got %s", cmd.Use)
	}

	for _, flag := range []string{"output", "chart", "show-uptodate", "fail-on-drift"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected flag %q to be registered", flag)
		}
	}
}

func TestScanRefs_Basic(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, "app1", "mychart", "1.0.0")
	writeTestManifest(t, dir, "app2", "otherchart", "2.0.0")

	cfg := config.DefaultConfig()
	refs, err := scanRefs(cfg, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
}

func TestScanRefs_WithFilter(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, "app1", "mychart", "1.0.0")
	writeTestManifest(t, dir, "app2", "otherchart", "2.0.0")

	cfg := config.DefaultConfig()
	refs, err := scanRefs(cfg, dir, "mychart")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 1 {
		t.Fatalf("expected 1 ref (filtered), got %d", len(refs))
	}
	if refs[0].ChartName != "mychart" {
		t.Errorf("expected mychart, got %s", refs[0].ChartName)
	}
}

func TestScanRefs_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	refs, err := scanRefs(cfg, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 0 {
		t.Errorf("expected 0 refs in empty dir, got %d", len(refs))
	}
}

func TestScanRefs_NonExistentDir(t *testing.T) {
	cfg := config.DefaultConfig()
	refs, err := scanRefs(cfg, "/nonexistent/path", "")
	if err != nil {
		return // error is acceptable
	}
	// walker may warn but not error, so check refs are empty
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for non-existent dir, got %d", len(refs))
	}
}

func TestCheckVersions(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()

	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.0.0", Type: manifest.SourceTypeHTTP, FilePath: "test.yaml"},
	}

	results := checkVersions(context.Background(), cfg, refs)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.ChartName != "mychart" {
		t.Errorf("expected mychart, got %s", r.ChartName)
	}
	if r.CurrentVersion != "1.0.0" {
		t.Errorf("expected current 1.0.0, got %s", r.CurrentVersion)
	}
	if r.LatestVersion != "1.2.0" {
		t.Errorf("expected latest 1.2.0, got %s", r.LatestVersion)
	}
	if r.Status != output.StatusUpdateAvailable {
		t.Errorf("expected status %q, got %q", output.StatusUpdateAvailable, r.Status)
	}
}

func TestCheckVersions_UpToDate(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()

	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: srv.URL, TargetRevision: "1.2.0", Type: manifest.SourceTypeHTTP, FilePath: "test.yaml"},
	}

	results := checkVersions(context.Background(), cfg, refs)

	if results[0].Status != output.StatusUpToDate {
		t.Errorf("expected up-to-date status, got %q", results[0].Status)
	}
}

func TestCheckVersions_MajorBump(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()

	refs := []manifest.ChartReference{
		{ChartName: "otherchart", RepoURL: srv.URL, TargetRevision: "2.0.0", Type: manifest.SourceTypeHTTP, FilePath: "test.yaml"},
	}

	results := checkVersions(context.Background(), cfg, refs)

	if results[0].Status != output.StatusBreaking {
		t.Errorf("expected breaking status, got %q", results[0].Status)
	}
}

func TestCheckVersions_RegistryError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()

	refs := []manifest.ChartReference{
		{ChartName: "mychart", RepoURL: server.URL, TargetRevision: "1.0.0", Type: manifest.SourceTypeHTTP, FilePath: "test.yaml"},
	}

	results := checkVersions(context.Background(), cfg, refs)

	if results[0].Status != output.StatusError {
		t.Errorf("expected error status, got %q", results[0].Status)
	}
	if results[0].LatestVersion != "?" {
		t.Errorf("expected '?' for latest version on error, got %q", results[0].LatestVersion)
	}
}

func TestRunScan_Integration(t *testing.T) {
	srv := newTestHelmServer(t)
	dir := t.TempDir()

	// Write a manifest that references the test server
	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.0.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runScan(context.Background(), "", dir, "", "table", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunScan_FailOnDrift(t *testing.T) {
	srv := newTestHelmServer(t)
	dir := t.TempDir()

	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.0.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runScan(context.Background(), "", dir, "", "table", false, true)
	if err == nil {
		t.Error("expected error with fail-on-drift when chart is outdated")
	}
}

func TestRunScan_NoDrift(t *testing.T) {
	srv := newTestHelmServer(t)
	dir := t.TempDir()

	content := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: myapp\nspec:\n  source:\n    repoURL: " + srv.URL + "\n    chart: mychart\n    targetRevision: 1.2.0\n"
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runScan(context.Background(), "", dir, "", "json", false, true)
	if err != nil {
		t.Fatalf("unexpected error (no drift): %v", err)
	}
}

func TestResolveLatest(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	factory := registry.NewFactory(cfg, "")

	ref := &manifest.ChartReference{
		ChartName: "mychart",
		RepoURL:   srv.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	latest, versions, _, err := resolveLatest(context.Background(), factory, cfg, ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if latest != "1.2.0" {
		t.Errorf("expected 1.2.0, got %s", latest)
	}
	if len(versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(versions))
	}
}

func TestResolveLatest_ChartNotFound(t *testing.T) {
	srv := newTestHelmServer(t)
	cfg := config.DefaultConfig()
	factory := registry.NewFactory(cfg, "")

	ref := &manifest.ChartReference{
		ChartName: "nonexistent",
		RepoURL:   srv.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	_, _, _, err := resolveLatest(context.Background(), factory, cfg, ref)
	if err == nil {
		t.Error("expected error for nonexistent chart")
	}
}
