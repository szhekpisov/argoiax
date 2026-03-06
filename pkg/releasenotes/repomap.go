package releasenotes

import (
	"strings"

	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/registry"
)

// MapChartToRepo determines the GitHub repository for a chart.
func MapChartToRepo(_, repoURL string, chartCfg *config.Chart) GitHubRepo {
	// Check explicit config override first
	if chartCfg != nil && chartCfg.GithubRepo != "" {
		parts := strings.SplitN(chartCfg.GithubRepo, "/", 2)
		if len(parts) == 2 {
			return GitHubRepo{Owner: parts[0], Repo: parts[1]}
		}
	}

	// Try heuristic mapping
	return heuristicMap(repoURL)
}

func heuristicMap(repoURL string) GitHubRepo {
	// Pattern: https://<org>.github.io/<repo>/
	if strings.Contains(repoURL, ".github.io/") {
		return fromGitHubPages(repoURL)
	}

	// Pattern: oci://ghcr.io/<org>/<path>
	if strings.HasPrefix(repoURL, "oci://ghcr.io/") {
		return fromGHCR(repoURL)
	}

	// Pattern: https://github.com/<org>/<repo>.git or https://github.com/<org>/<repo>
	if strings.Contains(repoURL, "github.com/") {
		return fromGitHubURL(repoURL)
	}

	return GitHubRepo{}
}

func fromGitHubPages(url string) GitHubRepo {
	// https://prometheus-community.github.io/helm-charts/
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")

	parts := strings.SplitN(url, ".github.io/", 2)
	if len(parts) != 2 {
		return GitHubRepo{}
	}

	org := parts[0]
	repo := strings.Trim(parts[1], "/")

	// Handle cases where the path after github.io has subpaths
	if idx := strings.Index(repo, "/"); idx != -1 {
		repo = repo[:idx]
	}

	if repo == "" {
		repo = org
	}

	return GitHubRepo{Owner: org, Repo: repo}
}

func fromGHCR(url string) GitHubRepo {
	// oci://ghcr.io/org/charts/chartname → github.com/org/charts
	path := strings.TrimPrefix(url, "oci://ghcr.io/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return GitHubRepo{}
	}
	return GitHubRepo{Owner: parts[0], Repo: parts[1]}
}

func fromGitHubURL(url string) GitHubRepo {
	owner, repo := registry.ExtractGitHubOwnerRepo(url)
	if owner == "" {
		return GitHubRepo{}
	}
	return GitHubRepo{Owner: owner, Repo: repo}
}
