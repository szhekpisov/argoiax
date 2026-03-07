package releasenotes

import (
	"context"
	"testing"

	"github.com/vertrost/argoiax/pkg/config"
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
