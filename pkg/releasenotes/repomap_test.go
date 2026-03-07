package releasenotes

import (
	"testing"

	"github.com/szhekpisov/argoiax/pkg/config"
)

func TestMapChartToRepo_ExplicitConfig(t *testing.T) {
	cfg := &config.Chart{GithubRepo: "bitnami/charts"}

	repo := MapChartToRepo("postgresql", "https://charts.bitnami.com/bitnami", cfg)
	if repo.Owner != "bitnami" || repo.Repo != "charts" {
		t.Errorf("expected bitnami/charts, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_GitHubPages(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		repo  string
	}{
		{"https://prometheus-community.github.io/helm-charts/", "prometheus-community", "helm-charts"},
		{"https://grafana.github.io/helm-charts", "grafana", "helm-charts"},
		{"https://kubernetes.github.io/ingress-nginx", "kubernetes", "ingress-nginx"},
	}

	for _, tt := range tests {
		repo := MapChartToRepo("", tt.url, nil)
		if repo.Owner != tt.owner || repo.Repo != tt.repo {
			t.Errorf("MapChartToRepo(%q): expected %s/%s, got %s/%s", tt.url, tt.owner, tt.repo, repo.Owner, repo.Repo)
		}
	}
}

func TestMapChartToRepo_GHCR(t *testing.T) {
	repo := MapChartToRepo("", "oci://ghcr.io/myorg/charts/mychart", nil)
	if repo.Owner != "myorg" || repo.Repo != "charts" {
		t.Errorf("expected myorg/charts, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_GitHubURL(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		repo  string
	}{
		{"https://github.com/myorg/helm-charts.git", "myorg", "helm-charts"},
		{"https://github.com/myorg/helm-charts", "myorg", "helm-charts"},
	}

	for _, tt := range tests {
		repo := MapChartToRepo("", tt.url, nil)
		if repo.Owner != tt.owner || repo.Repo != tt.repo {
			t.Errorf("MapChartToRepo(%q): expected %s/%s, got %s/%s", tt.url, tt.owner, tt.repo, repo.Owner, repo.Repo)
		}
	}
}

func TestMapChartToRepo_GHCRShortPath(t *testing.T) {
	repo := MapChartToRepo("", "oci://ghcr.io/onlyone", nil)
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for short GHCR path, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_GitHubPagesHTTP(t *testing.T) {
	repo := MapChartToRepo("", "http://org.github.io/repo", nil)
	if repo.Owner != "org" || repo.Repo != "repo" {
		t.Errorf("expected org/repo, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_GitHubPagesEmptyRepo(t *testing.T) {
	repo := MapChartToRepo("", "https://org.github.io/", nil)
	if repo.Owner != "org" || repo.Repo != "org" {
		t.Errorf("expected org/org when repo path is empty, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_InvalidGithubRepoConfig(t *testing.T) {
	cfg := &config.Chart{GithubRepo: "invalid"}
	repo := MapChartToRepo("chart", "https://example.com", cfg)
	// When GithubRepo has no slash, the explicit config path does not produce
	// a valid owner/repo pair and falls through to heuristic mapping.
	// The URL "https://example.com" doesn't match any heuristic, so result is empty.
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for invalid GithubRepo config, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestFromGitHubURL_Empty(t *testing.T) {
	// A URL that contains "github.com/" but has no valid owner/repo pair
	repo := MapChartToRepo("", "https://not-a-github-url.example.com/charts", nil)
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for non-GitHub URL, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_UnknownURL(t *testing.T) {
	repo := MapChartToRepo("", "https://custom-registry.example.com/charts", nil)
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for unknown URL, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_GitHubURLShortPath(t *testing.T) {
	// github.com URL with only one path segment
	repo := MapChartToRepo("", "https://github.com/onlyone", nil)
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for short GitHub path, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestMapChartToRepo_GitHubPagesSubpath(t *testing.T) {
	repo := MapChartToRepo("", "https://org.github.io/charts/subpath/more", nil)
	if repo.Owner != "org" || repo.Repo != "charts" {
		t.Errorf("expected org/charts, got %s/%s", repo.Owner, repo.Repo)
	}
}

func TestFromGitHubPages_NoSlashAfterDomain(t *testing.T) {
	// URL with .github.io but no "/" after it — won't match ".github.io/" pattern
	repo := MapChartToRepo("", "https://invalid-format.github.io", nil)
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for URL without .github.io/ pattern, got %s/%s", repo.Owner, repo.Repo)
	}
}
