package releasenotes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChangelogFetcher_Name(t *testing.T) {
	f := NewChangelogFetcher(nil)
	if got := f.Name(); got != "changelog" {
		t.Errorf("Name() = %q, want %q", got, "changelog")
	}
}

func TestChangelogFetcher_Fetch_Success(t *testing.T) {
	changelog := `# Changelog

## [1.3.0] - 2024-01-15
- Added widget support
- Fixed crash on startup

## [1.2.0] - 2024-01-01
- Initial stable release
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/myorg/myrepo/main/CHANGELOG.md" {
			_, _ = w.Write([]byte(changelog))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://raw.githubusercontent.com")
	f := NewChangelogFetcher(client)

	entries, sourceURL, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.3.0", "1.2.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Version != "1.3.0" {
		t.Errorf("entries[0].Version = %q, want %q", entries[0].Version, "1.3.0")
	}
	if entries[1].Version != "1.2.0" {
		t.Errorf("entries[1].Version = %q, want %q", entries[1].Version, "1.2.0")
	}

	if sourceURL == "" {
		t.Error("expected non-empty sourceURL")
	}
}

func TestChangelogFetcher_Fetch_MasterBranch(t *testing.T) {
	changelog := `# Changelog

## 2.0.0
- Breaking change
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/myorg/myrepo/master/CHANGELOG.md" {
			_, _ = w.Write([]byte(changelog))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://raw.githubusercontent.com")
	f := NewChangelogFetcher(client)

	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"2.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestChangelogFetcher_Fetch_NoChangelog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://raw.githubusercontent.com")
	f := NewChangelogFetcher(client)

	_, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err == nil {
		t.Error("expected error when no changelog found")
	}
}

func TestChangelogFetcher_Fetch_VersionNotInChangelog(t *testing.T) {
	changelog := `# Changelog

## [1.0.0]
- First release
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/myorg/myrepo/main/CHANGELOG.md" {
			_, _ = w.Write([]byte(changelog))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://raw.githubusercontent.com")
	f := NewChangelogFetcher(client)

	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"9.9.9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for missing version, got %d", len(entries))
	}
}

func TestExtractVersionSection(t *testing.T) {
	changelog := `# Changelog

## [1.3.0] - 2024-01-15
- Added widget support
- Fixed crash on startup

## [1.2.0] - 2024-01-01
- Initial stable release

## [1.1.0] - 2023-12-01
- Beta features
`

	tests := []struct {
		version  string
		contains string
		empty    bool
	}{
		{"1.3.0", "Added widget support", false},
		{"1.2.0", "Initial stable release", false},
		{"1.1.0", "Beta features", false},
		{"9.9.9", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			section := extractVersionSection(changelog, tt.version)
			if tt.empty {
				if section != "" {
					t.Errorf("expected empty section, got %q", section)
				}
				return
			}
			if section == "" {
				t.Fatal("expected non-empty section")
			}
			if tt.contains != "" && !contains(section, tt.contains) {
				t.Errorf("section %q does not contain %q", section, tt.contains)
			}
		})
	}
}

func TestExtractVersionSection_LastEntry(t *testing.T) {
	changelog := `## 1.0.0
- Only release`

	section := extractVersionSection(changelog, "1.0.0")
	if section != "- Only release" {
		t.Errorf("unexpected section: %q", section)
	}
}

func TestContainsVersion(t *testing.T) {
	tests := []struct {
		header  string
		version string
		want    bool
	}{
		{"## [1.2.3] - 2024-01-01", "1.2.3", true},
		{"## v1.2.3", "1.2.3", true},
		{"## 1.2.30", "1.2.3", false},
		{"## 01.2.3", "1.2.3", false},
		{"## something else", "1.2.3", false},
		{"## 1.2.3", "1.2.3", true},
	}

	for _, tt := range tests {
		t.Run(tt.header+"_"+tt.version, func(t *testing.T) {
			got := containsVersion(tt.header, tt.version)
			if got != tt.want {
				t.Errorf("containsVersion(%q, %q) = %v, want %v", tt.header, tt.version, got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
