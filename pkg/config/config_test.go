package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.ScanDirs) != 1 || cfg.ScanDirs[0] != "." {
		t.Errorf("expected scanDirs [.], got %v", cfg.ScanDirs)
	}
	if cfg.Settings.PRStrategy != "per-chart" {
		t.Errorf("expected prStrategy per-chart, got %s", cfg.Settings.PRStrategy)
	}
	if cfg.Settings.MaxOpenPRs != 10 {
		t.Errorf("expected maxOpenPRs 10, got %d", cfg.Settings.MaxOpenPRs)
	}
	if !cfg.Release.Enabled {
		t.Error("expected releaseNotes.enabled to be true")
	}
}

func TestLoad_DefaultWhenMissing(t *testing.T) {
	// When no config file exists, should return defaults
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected default version 1, got %d", cfg.Version)
	}
}

func TestLoad_ExplicitMissing(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing explicit config")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "argoiax.yaml")

	content := `
version: 1
scanDirs: [apps/]
charts:
  cert-manager:
    versionConstraint: ">=1.0.0, <2.0.0"
settings:
  prStrategy: per-file
  baseBranch: develop
  maxOpenPRs: 5
releaseNotes:
  enabled: true
  sources: [github-releases]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.ScanDirs) != 1 || cfg.ScanDirs[0] != "apps/" {
		t.Errorf("expected scanDirs [apps/], got %v", cfg.ScanDirs)
	}
	if cfg.Settings.PRStrategy != "per-file" {
		t.Errorf("expected prStrategy per-file, got %s", cfg.Settings.PRStrategy)
	}
	if cfg.Settings.BaseBranch != "develop" {
		t.Errorf("expected baseBranch develop, got %s", cfg.Settings.BaseBranch)
	}
	if cfg.Settings.MaxOpenPRs != 5 {
		t.Errorf("expected maxOpenPRs 5, got %d", cfg.Settings.MaxOpenPRs)
	}

	chart := cfg.LookupChart("cert-manager", "")
	if chart == nil {
		t.Fatal("expected to find cert-manager chart config")
	}
	if chart.VersionConstraint != ">=1.0.0, <2.0.0" {
		t.Errorf("expected constraint >=1.0.0, <2.0.0, got %s", chart.VersionConstraint)
	}
}

func TestLoad_ExpandsEnvVars(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "argoiax.yaml")

	t.Setenv("TEST_HELM_USER", "myuser")
	t.Setenv("TEST_HELM_PASS", "mypass")

	content := `
version: 1
auth:
  helmRepos:
    - url: "https://private.example.com"
      username: "${TEST_HELM_USER}"
      password: "${TEST_HELM_PASS}"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Auth.HelmRepos) != 1 {
		t.Fatalf("expected 1 helm repo auth, got %d", len(cfg.Auth.HelmRepos))
	}
	if cfg.Auth.HelmRepos[0].Username != "myuser" {
		t.Errorf("expected username myuser, got %s", cfg.Auth.HelmRepos[0].Username)
	}
	if cfg.Auth.HelmRepos[0].Password != "mypass" {
		t.Errorf("expected password mypass, got %s", cfg.Auth.HelmRepos[0].Password)
	}
}

func TestValidate_InvalidStrategy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings.PRStrategy = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid prStrategy")
	}
}

func TestValidate_InvalidReleaseSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Release.Sources = []string{"invalid-source"}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid release notes source")
	}
}

func TestValidate_InvalidBranchTemplate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings.BranchTemplate = "{{.Broken"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid branchTemplate")
	}
}

func TestValidate_InvalidTitleTemplate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings.TitleTemplate = "{{.Broken"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid titleTemplate")
	}
}

func TestValidate_ValidTemplates(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestLookupChart(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Charts = map[string]Chart{
		"cert-manager":                   {VersionConstraint: ">=1.0.0"},
		"https://bitnami.com#postgresql": {GithubRepo: "bitnami/charts"},
	}

	// By name
	ch := cfg.LookupChart("cert-manager", "")
	if ch == nil || ch.VersionConstraint != ">=1.0.0" {
		t.Error("expected to find cert-manager by name")
	}

	// By repoURL#name
	ch = cfg.LookupChart("postgresql", "https://bitnami.com")
	if ch == nil || ch.GithubRepo != "bitnami/charts" {
		t.Error("expected to find postgresql by repoURL#name")
	}

	// Not found
	ch = cfg.LookupChart("nonexistent", "")
	if ch != nil {
		t.Error("expected nil for nonexistent chart")
	}
}
