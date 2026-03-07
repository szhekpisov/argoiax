package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/szhekpisov/argoiax/pkg/manifest"
)

// GitRegistry implements Registry for Git-based sources using the GitHub/GitLab API.
type GitRegistry struct {
	client *http.Client
}

// NewGitRegistry creates a new GitRegistry with the given token.
func NewGitRegistry(token string) *GitRegistry {
	return &GitRegistry{
		client: NewTokenClient(token),
	}
}

type gitTag struct {
	Name string `json:"name"`
}

// ListVersions returns all available tags from a GitHub repository.
func (r *GitRegistry) ListVersions(ctx context.Context, ref *manifest.ChartReference) ([]string, error) {
	owner, repo := ExtractGitHubOwnerRepo(ref.RepoURL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("cannot extract GitHub owner/repo from %s", ref.RepoURL)
	}

	slog.Debug("listing git tags", "owner", owner, "repo", repo)

	var versions []string
	nextURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100", owner, repo)

	for nextURL != "" {
		tags, next, err := r.fetchTagPage(ctx, nextURL)
		if err != nil {
			return nil, err
		}
		for _, t := range tags {
			versions = append(versions, t.Name)
		}
		nextURL = next
	}

	return versions, nil
}

func (r *GitRegistry) fetchTagPage(ctx context.Context, apiURL string) ([]gitTag, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := r.client.Do(req) //nolint:bodyclose // closed via DrainBody
	if err != nil {
		return nil, "", fmt.Errorf("fetching tags from GitHub: %w", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GitHub API returned status %d for %s", resp.StatusCode, apiURL)
	}

	var tags []gitTag
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, "", fmt.Errorf("decoding GitHub tags response: %w", err)
	}

	return tags, parseNextLink(resp.Header.Get("Link")), nil
}

// parseNextLink extracts the "next" URL from a GitHub Link header.
func parseNextLink(header string) string {
	if header == "" {
		return ""
	}
	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, `rel="next"`) {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start >= 0 && end > start {
				return part[start+1 : end]
			}
		}
	}
	return ""
}

// ExtractGitHubOwnerRepo extracts the owner and repo from a GitHub URL.
// Handles https, http, git@github.com:, and github.com/ prefixes.
func ExtractGitHubOwnerRepo(repoURL string) (owner, repo string) {
	url := repoURL
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@github.com:")
	url = strings.TrimPrefix(url, "github.com/")

	parts := strings.SplitN(url, "/", 3)
	if len(parts) < 2 {
		return "", ""
	}

	return parts[0], parts[1]
}
