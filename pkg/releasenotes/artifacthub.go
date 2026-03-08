package releasenotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/registry"
)

// ArtifactHubFetcher retrieves release notes from the ArtifactHub API.
type ArtifactHubFetcher struct {
	client *http.Client
}

// NewArtifactHubFetcher creates a new ArtifactHubFetcher with the given HTTP client.
func NewArtifactHubFetcher(client *http.Client) *ArtifactHubFetcher {
	return &ArtifactHubFetcher{
		client: client,
	}
}

// Name returns the source name.
func (f *ArtifactHubFetcher) Name() string { return config.SourceArtifactHub }

// Fetch retrieves release notes from ArtifactHub for the given versions.
func (f *ArtifactHubFetcher) Fetch(ctx context.Context, repo GitHubRepo, versions []string) ([]Entry, string, error) {
	if repo.ChartName == "" {
		return nil, "", nil
	}

	var entries []Entry
	sourceURL := ""

	for _, version := range versions {
		entry, url, err := f.fetchVersion(ctx, repo, version)
		if err != nil {
			continue
		}
		if entry != nil {
			entries = append(entries, *entry)
			if sourceURL == "" {
				sourceURL = url
			}
		}
	}

	return entries, sourceURL, nil
}

type artifactHubPackage struct {
	Version string              `json:"version"`
	Changes []artifactHubChange `json:"changes"`
	HTMLURL string              `json:"package_id"`
}

type artifactHubChange struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

func (f *ArtifactHubFetcher) fetchVersion(ctx context.Context, repo GitHubRepo, version string) (*Entry, string, error) {
	// Try common ArtifactHub repo/package patterns.
	// ArtifactHub URLs are helm/{artifacthub-repo}/{chart-name}.
	// The ArtifactHub repo name often matches the chart name or the GitHub owner.
	seen := make(map[string]bool)
	var patterns []string
	addPattern := func(repoName, chartName string) {
		p := fmt.Sprintf("helm/%s/%s", repoName, chartName)
		if !seen[p] {
			seen[p] = true
			patterns = append(patterns, p)
		}
	}

	chartName := repo.ChartName
	addPattern(chartName, chartName)
	if repo.Owner != "" {
		addPattern(repo.Owner, chartName)
	}
	if repo.Repo != "" {
		addPattern(repo.Repo, chartName)
	}

	for _, pkg := range patterns {
		entry, pageURL, err := f.tryPackage(ctx, pkg, version)
		if err != nil {
			continue
		}
		if entry != nil {
			return entry, pageURL, nil
		}
	}

	return nil, "", errors.New("not found on ArtifactHub")
}

func (f *ArtifactHubFetcher) tryPackage(ctx context.Context, pkg, version string) (*Entry, string, error) {
	url := fmt.Sprintf("https://artifacthub.io/api/v1/packages/%s/%s", pkg, version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, "", err
	}

	resp, err := f.client.Do(req) //nolint:bodyclose // closed via registry.DrainBody
	if err != nil {
		return nil, "", err
	}
	defer registry.DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, "", nil
	}

	var ahPkg artifactHubPackage
	if err := json.NewDecoder(resp.Body).Decode(&ahPkg); err != nil {
		return nil, "", err
	}

	if len(ahPkg.Changes) == 0 {
		return nil, "", nil
	}

	var body strings.Builder
	for _, change := range ahPkg.Changes {
		prefix := "- "
		if change.Kind != "" {
			prefix = fmt.Sprintf("- [%s] ", change.Kind)
		}
		body.WriteString(prefix + change.Description + "\n")
	}

	pageURL := fmt.Sprintf("https://artifacthub.io/packages/%s/%s", pkg, version)
	return &Entry{
		Version: version,
		Body:    body.String(),
		URL:     pageURL,
	}, pageURL, nil
}
