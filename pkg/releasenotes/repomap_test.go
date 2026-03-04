package releasenotes

import (
	"testing"

	"github.com/vertrost/argoiax/pkg/config"
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

func TestMapChartToRepo_UnknownURL(t *testing.T) {
	repo := MapChartToRepo("", "https://custom-registry.example.com/charts", nil)
	if repo.Owner != "" || repo.Repo != "" {
		t.Errorf("expected empty repo for unknown URL, got %s/%s", repo.Owner, repo.Repo)
	}
}
