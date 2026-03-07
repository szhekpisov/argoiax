package releasenotes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/registry"
)

var nextHeaderRe = regexp.MustCompile(`(?m)^#{1,3}\s+\[?v?\d+\.\d+`)

// ChangelogFetcher retrieves release notes from CHANGELOG.md files in GitHub repos.
type ChangelogFetcher struct {
	client *http.Client
}

// NewChangelogFetcher creates a new ChangelogFetcher with the given HTTP client.
func NewChangelogFetcher(client *http.Client) *ChangelogFetcher {
	return &ChangelogFetcher{
		client: client,
	}
}

// Name returns the source name.
func (f *ChangelogFetcher) Name() string { return config.SourceChangelog }

// Fetch retrieves release notes from a CHANGELOG.md file in the given repo.
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

func (f *ChangelogFetcher) fetchChangelogContent(ctx context.Context, repo GitHubRepo) (content, rawURL string, err error) {
	filenames := []string{"CHANGELOG.md", "changelog.md", "CHANGES.md", "HISTORY.md"}
	branches := []string{"main", "master"}

	for _, filename := range filenames {
		for _, branch := range branches {
			body, err := f.tryChangelogFile(ctx, repo, branch, filename)
			if err != nil {
				continue
			}
			if body != "" {
				return body, branch, nil
			}
		}
	}

	return "", "", fmt.Errorf("no changelog found for %s/%s", repo.Owner, repo.Repo)
}

func (f *ChangelogFetcher) tryChangelogFile(ctx context.Context, repo GitHubRepo, branch, filename string) (string, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", repo.Owner, repo.Repo, branch, filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}

	resp, err := f.client.Do(req) //nolint:bodyclose // closed via registry.DrainBody
	if err != nil {
		return "", err
	}
	defer registry.DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	const maxChangelogSize = 5 * 1024 * 1024 // 5 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChangelogSize))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// extractVersionSection extracts the section for a specific version from changelog content.
// It uses the pre-compiled nextHeaderRe to find all version headers, then identifies the
// target version via string matching, avoiding per-call regex compilation.
func extractVersionSection(content, version string) string {
	locs := nextHeaderRe.FindAllStringIndex(content, -1)

	for i, loc := range locs {
		// Extract the full header line.
		lineEnd := strings.IndexByte(content[loc[0]:], '\n')
		var headerLine string
		if lineEnd >= 0 {
			headerLine = content[loc[0] : loc[0]+lineEnd]
		} else {
			headerLine = content[loc[0]:]
		}

		if !containsVersion(headerLine, version) {
			continue
		}

		// Section starts after the header line.
		sectionStart := loc[0] + len(headerLine)
		if lineEnd >= 0 {
			sectionStart++ // skip the newline
		}

		var sectionEnd int
		if i+1 < len(locs) {
			sectionEnd = locs[i+1][0]
		} else {
			sectionEnd = len(content)
		}

		return strings.TrimSpace(content[sectionStart:sectionEnd])
	}

	return ""
}

// containsVersion reports whether a header line contains the exact version string,
// ensuring it's not part of a larger number (e.g., "1.2.3" must not match "1.2.30").
func containsVersion(header, version string) bool {
	i := strings.Index(header, version)
	if i < 0 {
		return false
	}
	end := i + len(version)
	if end < len(header) && header[end] >= '0' && header[end] <= '9' {
		return false
	}
	if i > 0 && header[i-1] >= '0' && header[i-1] <= '9' {
		return false
	}
	return true
}
