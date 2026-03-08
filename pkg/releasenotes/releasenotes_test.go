package releasenotes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/config"
)

func TestCombinedBody_Nil(t *testing.T) {
	var n *Notes
	if got := n.CombinedBody(); got != "" {
		t.Errorf("expected empty string for nil Notes, got %q", got)
	}
}

func TestCombinedBody_MultipleEntries(t *testing.T) {
	n := &Notes{
		Entries: []Entry{
			{Version: "1.0.0", Body: "first"},
			{Version: "1.1.0", Body: "second"},
		},
	}
	got := n.CombinedBody()
	want := "first\nsecond"
	if got != want {
		t.Errorf("CombinedBody() = %q, want %q", got, want)
	}
}

func TestCombinedBody_Empty(t *testing.T) {
	n := &Notes{}
	if got := n.CombinedBody(); got != "" {
		t.Errorf("expected empty string for zero entries, got %q", got)
	}
}

func TestOrchestrator_Truncate(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{MaxLength: 10},
	}

	notes := &Notes{
		Entries: []Entry{
			{Version: "1.0.0", Body: "short"},
			{Version: "1.1.0", Body: "this is a longer body that exceeds the limit"},
		},
	}

	o.truncate(notes)

	if len(notes.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(notes.Entries))
	}
	// First entry is 5 runes, leaving 5 for second entry
	if got := notes.Entries[1].Body; got != "this \n\n... (truncated)" {
		t.Errorf("truncated body = %q", got)
	}
}

func TestOrchestrator_TruncateNoOp(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{MaxLength: 0},
	}

	notes := &Notes{
		Entries: []Entry{{Version: "1.0.0", Body: "hello"}},
	}

	o.truncate(notes)

	if notes.Entries[0].Body != "hello" {
		t.Errorf("expected no truncation when MaxLength=0, got %q", notes.Entries[0].Body)
	}
}

func TestOrchestrator_TruncateDropsEntries(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{MaxLength: 3},
	}

	notes := &Notes{
		Entries: []Entry{
			{Version: "1.0.0", Body: "abcd"},
			{Version: "1.1.0", Body: "efgh"},
		},
	}

	o.truncate(notes)

	if len(notes.Entries) != 1 {
		t.Fatalf("expected 1 entry after truncation, got %d", len(notes.Entries))
	}
	if got := notes.Entries[0].Body; got != "abc\n\n... (truncated)" {
		t.Errorf("truncated body = %q", got)
	}
}

func TestNewOrchestrator_Sources(t *testing.T) {
	cfg := config.ReleaseNotesConfig{
		Enabled: true,
		Sources: []string{
			config.SourceGitHubReleases,
			config.SourceArtifactHub,
			config.SourceChangelog,
		},
	}

	o := NewOrchestrator(cfg, "fake-token")

	if len(o.fetchers) != 3 {
		t.Fatalf("expected 3 fetchers, got %d", len(o.fetchers))
	}

	names := []string{
		config.SourceGitHubReleases,
		config.SourceArtifactHub,
		config.SourceChangelog,
	}
	for i, f := range o.fetchers {
		if f.Name() != names[i] {
			t.Errorf("fetcher[%d].Name() = %q, want %q", i, f.Name(), names[i])
		}
	}
}

func TestNewOrchestrator_NoToken(t *testing.T) {
	cfg := config.ReleaseNotesConfig{
		Enabled: true,
		Sources: []string{config.SourceGitHubReleases},
	}

	o := NewOrchestrator(cfg, "")
	if len(o.fetchers) != 1 {
		t.Fatalf("expected 1 fetcher, got %d", len(o.fetchers))
	}
}

func TestOrchestrator_FetchNotes_Disabled(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{Enabled: false},
	}

	got := o.FetchNotes(context.Background(), "chart", "https://example.com", []string{"1.0.0"}, nil)
	if got != nil {
		t.Errorf("expected nil when disabled, got %+v", got)
	}
}

func TestOrchestrator_FetchNotes_NoRepoMapping(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{Enabled: true},
		fetchers: []Fetcher{
			NewGitHubFetcher(nil), // won't be called
		},
	}

	got := o.FetchNotes(context.Background(), "chart", "https://custom-registry.example.com/charts", []string{"1.0.0"}, nil)
	if got != nil {
		t.Errorf("expected nil for unmappable repo, got %+v", got)
	}
}

type stubFetcher struct {
	name    string
	entries []Entry
	url     string
	err     error
}

func (f *stubFetcher) Fetch(_ context.Context, _ GitHubRepo, _ []string) ([]Entry, string, error) {
	return f.entries, f.url, f.err
}
func (f *stubFetcher) Name() string { return f.name }

type captureFetcher struct {
	entries []Entry
	url     string
	calls   *[]struct{ repo GitHubRepo }
}

func (f *captureFetcher) Fetch(_ context.Context, repo GitHubRepo, _ []string) ([]Entry, string, error) {
	*f.calls = append(*f.calls, struct{ repo GitHubRepo }{repo: repo})
	return f.entries, f.url, nil
}

func (f *captureFetcher) Name() string { return "capture" }

func newDiscoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/packages/helm/datadog/datadog" {
			_ = json.NewEncoder(w).Encode(artifactHubPackage{
				ContentURL: "https://github.com/DataDog/helm-charts/releases/download/datadog-3.181.1/datadog-3.181.1.tgz",
			})
			return
		}
		http.NotFound(w, r)
	}))
}

func TestOrchestrator_FetchNotes_FetcherError(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{Enabled: true, MaxLength: 0},
		fetchers: []Fetcher{
			&stubFetcher{name: "failing", err: errors.New("connection refused")}, // returns error
			&stubFetcher{
				name:    "fallback",
				entries: []Entry{{Version: "1.0.0", Body: "notes from fallback"}},
				url:     "https://example.com/fallback",
			},
		},
	}

	got := o.FetchNotes(context.Background(), "chart", "https://grafana.github.io/helm-charts", []string{"1.0.0"}, nil)
	if got == nil {
		t.Fatal("expected non-nil notes from fallback fetcher")
	}
	if len(got.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got.Entries))
	}
	if got.Entries[0].Body != "notes from fallback" {
		t.Errorf("unexpected body: %q", got.Entries[0].Body)
	}
	if got.SourceURL != "https://example.com/fallback" {
		t.Errorf("unexpected source URL: %s", got.SourceURL)
	}
}

func TestOrchestrator_FetchNotes_AllFetchersEmpty(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{Enabled: true},
		fetchers: []Fetcher{
			&stubFetcher{name: "first", entries: nil},
			&stubFetcher{name: "second", entries: []Entry{}},
		},
	}

	got := o.FetchNotes(context.Background(), "chart", "https://grafana.github.io/helm-charts", []string{"1.0.0"}, nil)
	if got != nil {
		t.Errorf("expected nil when all fetchers return empty, got %+v", got)
	}
}

func TestOrchestrator_FetchNotes_DiscoverRepo(t *testing.T) {
	var calls []struct{ repo GitHubRepo }

	capturingFetcher := &captureFetcher{
		entries: []Entry{{Version: "3.181.1", Body: "release notes"}},
		url:     "https://github.com/DataDog/helm-charts/releases/tag/datadog-3.181.1",
		calls:   &calls,
	}

	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{
			Enabled: true,
			Sources: []string{config.SourceArtifactHub, config.SourceGitHubReleases},
		},
		fetchers: []Fetcher{capturingFetcher},
	}

	// Set up ArtifactHub mock that returns content_url with GitHub URL
	server := newDiscoveryServer(t)
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	o.client = client

	got := o.FetchNotes(context.Background(), "datadog", "https://helm.datadoghq.com", []string{"3.181.1"}, nil)
	if got == nil {
		t.Fatal("expected non-nil notes")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 fetcher call, got %d", len(calls))
	}
	if calls[0].repo.Owner != "DataDog" {
		t.Errorf("expected Owner=DataDog, got %q", calls[0].repo.Owner)
	}
	if calls[0].repo.Repo != "helm-charts" {
		t.Errorf("expected Repo=helm-charts, got %q", calls[0].repo.Repo)
	}
}

func TestOrchestrator_FetchNotes_FallbackToSecondSource(t *testing.T) {
	o := &Orchestrator{
		cfg: config.ReleaseNotesConfig{Enabled: true, MaxLength: 0},
		fetchers: []Fetcher{
			&stubFetcher{name: "first", entries: nil}, // returns no entries
			&stubFetcher{
				name:    "second",
				entries: []Entry{{Version: "1.0.0", Body: "notes"}},
				url:     "https://example.com/releases",
			},
		},
	}

	got := o.FetchNotes(context.Background(), "chart", "https://grafana.github.io/helm-charts", []string{"1.0.0"}, nil)
	if got == nil {
		t.Fatal("expected non-nil notes")
	}
	if len(got.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got.Entries))
	}
	if got.SourceURL != "https://example.com/releases" {
		t.Errorf("unexpected source URL: %s", got.SourceURL)
	}
}
