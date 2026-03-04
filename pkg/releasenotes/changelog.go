package releasenotes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/vertrost/ancaeus/pkg/config"
	"github.com/vertrost/ancaeus/pkg/registry"
)

var nextHeaderRe = regexp.MustCompile(`(?m)^#{1,3}\s+\[?v?\d+\.\d+`)

// ChangelogFetcher retrieves release notes from CHANGELOG.md files in GitHub repos.
type ChangelogFetcher struct {
	client *http.Client
}

// NewChangelogFetcher creates a new ChangelogFetcher.
func NewChangelogFetcher(token string) *ChangelogFetcher {
	return &ChangelogFetcher{
		client: registry.NewTokenClient(token),
	}
}

func (f *ChangelogFetcher) Name() string { return config.SourceChangelog }

func (f *ChangelogFetcher) Fetch(ctx context.Context, repo GitHubRepo, versions []string) ([]Entry, string, error) {
	content, branch, err := f.fetchChangelogContent(ctx, repo)
	if err != nil {
		return nil, "", err
	}

	sourceURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/CHANGELOG.md", repo.Owner, repo.Repo, branch)
	var entries []Entry

	for _, version := range versions {
		section := extractVersionSection(content, version)
		if section != "" {
			entries = append(entries, Entry{
				Version: version,
				Body:    section,
				URL:     sourceURL,
			})
		}
	}

	return entries, sourceURL, nil
}

func (f *ChangelogFetcher) fetchChangelogContent(ctx context.Context, repo GitHubRepo) (string, string, error) {
	filenames := []string{"CHANGELOG.md", "changelog.md", "CHANGES.md", "HISTORY.md"}
	branches := []string{"main", "master"}

	for _, filename := range filenames {
		for _, branch := range branches {
			content, err := f.tryChangelogFile(ctx, repo, branch, filename)
			if err != nil {
				continue
			}
			if content != "" {
				return content, branch, nil
			}
		}
	}

	return "", "", fmt.Errorf("no changelog found for %s/%s", repo.Owner, repo.Repo)
}

func (f *ChangelogFetcher) tryChangelogFile(ctx context.Context, repo GitHubRepo, branch, filename string) (string, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", repo.Owner, repo.Repo, branch, filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer registry.DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// extractVersionSection extracts the section for a specific version from changelog content.
func extractVersionSection(content, version string) string {
	// Match version headers like "## 1.2.3", "## v1.2.3", "## [1.2.3]", "# 1.2.3"
	versionRe := regexp.MustCompile(
		fmt.Sprintf(`(?m)^#{1,3}\s+\[?v?%s\]?.*$`, regexp.QuoteMeta(version)),
	)

	loc := versionRe.FindStringIndex(content)
	if loc == nil {
		return ""
	}

	start := loc[1]

	// Find the next version header
	nextLoc := nextHeaderRe.FindStringIndex(content[start:])

	var section string
	if nextLoc != nil {
		section = content[start : start+nextLoc[0]]
	} else {
		section = content[start:]
	}

	return strings.TrimSpace(section)
}
