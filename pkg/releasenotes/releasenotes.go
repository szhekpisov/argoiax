package releasenotes

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/registry"
)

// Notes contains aggregated release notes for a chart update.
type Notes struct {
	Entries   []Entry
	SourceURL string
}

// CombinedBody joins all entry bodies into a single string.
func (n *Notes) CombinedBody() string {
	if n == nil {
		return ""
	}
	bodies := make([]string, len(n.Entries))
	for i, e := range n.Entries {
		bodies[i] = e.Body
	}
	return strings.Join(bodies, "\n")
}

// Entry is a single version's release notes.
type Entry struct {
	Version string
	Body    string
	URL     string
}

// Fetcher retrieves release notes from a specific source.
type Fetcher interface {
	// Fetch retrieves release notes for the given versions.
	Fetch(ctx context.Context, repo GitHubRepo, versions []string) ([]Entry, string, error)

	// Name returns the source name for logging.
	Name() string
}

// GitHubRepo identifies a GitHub repository.
type GitHubRepo struct {
	Owner string
	Repo  string
}

// Orchestrator coordinates multiple release note sources with fallback.
type Orchestrator struct {
	fetchers []Fetcher
	cfg      config.ReleaseNotesConfig
}

// NewOrchestrator creates a new release notes orchestrator with a shared HTTP client.
func NewOrchestrator(cfg config.ReleaseNotesConfig, githubToken string) *Orchestrator {
	var client *http.Client
	if githubToken != "" {
		client = registry.NewTokenClient(githubToken)
	} else {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	var fetchers []Fetcher
	for _, src := range cfg.Sources {
		switch src {
		case config.SourceGitHubReleases:
			fetchers = append(fetchers, NewGitHubFetcher(client))
		case config.SourceArtifactHub:
			fetchers = append(fetchers, NewArtifactHubFetcher(client))
		case config.SourceChangelog:
			fetchers = append(fetchers, NewChangelogFetcher(client))
		}
	}

	return &Orchestrator{
		fetchers: fetchers,
		cfg:      cfg,
	}
}

// FetchNotes retrieves release notes for a chart update, trying each source in order.
func (o *Orchestrator) FetchNotes(ctx context.Context, chartName, repoURL string, versions []string, chartCfg *config.Chart) *Notes {
	if !o.cfg.Enabled {
		return nil
	}

	const releaseNotesTimeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(ctx, releaseNotesTimeout)
	defer cancel()

	repo := MapChartToRepo(chartName, repoURL, chartCfg)
	if repo.Owner == "" || repo.Repo == "" {
		slog.Debug("could not map chart to GitHub repo", "chart", chartName, "repoURL", repoURL)
		return nil
	}

	for _, f := range o.fetchers {
		entries, sourceURL, err := f.Fetch(ctx, repo, versions)
		if err != nil {
			slog.Debug("release notes source failed", "source", f.Name(), "error", err)
			continue
		}

		if len(entries) > 0 {
			slog.Debug("fetched release notes", "source", f.Name(), "count", len(entries))
			notes := &Notes{
				Entries:   entries,
				SourceURL: sourceURL,
			}

			// Truncate if needed
			o.truncate(notes)
			return notes
		}
	}

	return nil
}

func (o *Orchestrator) truncate(notes *Notes) {
	if o.cfg.MaxLength <= 0 {
		return
	}

	remaining := o.cfg.MaxLength
	for i, e := range notes.Entries {
		bodyLen := utf8.RuneCountInString(e.Body)
		if bodyLen > remaining {
			notes.Entries = notes.Entries[:i+1]
			// Truncate at a rune boundary
			truncated := []rune(e.Body)[:remaining]
			notes.Entries[i].Body = string(truncated) + "\n\n... (truncated)"
			return
		}
		remaining -= bodyLen
	}
}
