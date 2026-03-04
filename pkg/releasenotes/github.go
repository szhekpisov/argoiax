package releasenotes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vertrost/ancaeus/pkg/config"
	"github.com/vertrost/ancaeus/pkg/registry"
)

// GitHubFetcher retrieves release notes from GitHub Releases API.
type GitHubFetcher struct {
	client *http.Client
}

// NewGitHubFetcher creates a new GitHubFetcher.
func NewGitHubFetcher(token string) *GitHubFetcher {
	return &GitHubFetcher{
		client: registry.NewTokenClient(token),
	}
}

func (f *GitHubFetcher) Name() string { return config.SourceGitHubReleases }

func (f *GitHubFetcher) Fetch(ctx context.Context, repo GitHubRepo, versions []string) ([]Entry, string, error) {
	var entries []Entry
	sourceURL := fmt.Sprintf("https://github.com/%s/%s/releases", repo.Owner, repo.Repo)

	for _, version := range versions {
		entry, err := f.fetchRelease(ctx, repo, version)
		if err != nil {
			slog.Debug("failed to fetch release", "version", version, "error", err)
			continue
		}
		if entry != nil {
			entries = append(entries, *entry)
		}
	}

	return entries, sourceURL, nil
}

func (f *GitHubFetcher) fetchRelease(ctx context.Context, repo GitHubRepo, version string) (*Entry, error) {
	// Try multiple tag patterns
	tagPatterns := []string{
		version,
		"v" + version,
		fmt.Sprintf("%s-%s", repo.Repo, version),
	}

	for _, tag := range tagPatterns {
		entry, err := f.fetchReleaseByTag(ctx, repo, tag, version)
		if err == nil && entry != nil {
			return entry, nil
		}
	}

	return nil, fmt.Errorf("no release found for %s", version)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

func (f *GitHubFetcher) fetchReleaseByTag(ctx context.Context, repo GitHubRepo, tag, version string) (*Entry, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", repo.Owner, repo.Repo, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer registry.DrainBody(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release not found for tag %s", tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &Entry{
		Version: version,
		Body:    release.Body,
		URL:     release.HTMLURL,
	}, nil
}
