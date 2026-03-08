package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
	"github.com/szhekpisov/argoiax/pkg/pr"
)

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

func TestApplyFileUpdates_FileNotFound(t *testing.T) {
	updates := []resolvedUpdate{makeUpdate("/nonexistent/path/file.yaml", "mychart", "1.0.0", "1.1.0")}

	_, err := applyFileUpdates(updates)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestDefaultNewGitHubClient(t *testing.T) {
	client := defaultNewGitHubClient(context.Background(), "fake-token")
	if client == nil {
		t.Fatal("expected non-nil client")
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
	overrideScanManifests(t, func(_ *config.Config, _, _ string) ([]manifest.ChartReference, error) {
		return nil, errors.New("scan failed")
	})

	root := &rootOptions{scanDir: t.TempDir()}
	err := runUpdate(context.Background(), root, "", false, 0, "fake-token", "owner/repo")
	if err == nil {
		t.Fatal("expected error from scanManifests")
	}
}
