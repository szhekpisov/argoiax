package releasenotes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArtifactHubFetcher_Name(t *testing.T) {
	f := NewArtifactHubFetcher(nil)
	if got := f.Name(); got != "artifacthub" {
		t.Errorf("Name() = %q, want %q", got, "artifacthub")
	}
}

func TestArtifactHubFetcher_Fetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match one of the expected patterns
		if r.URL.Path == "/api/v1/packages/helm/myrepo/myrepo/1.2.0" {
			_ = json.NewEncoder(w).Encode(artifactHubPackage{
				Version: "1.2.0",
				Changes: []artifactHubChange{
					{Kind: "added", Description: "New feature"},
					{Kind: "fixed", Description: "Bug fix"},
					{Kind: "", Description: "Other change"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, sourceURL, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.2.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Version != "1.2.0" {
		t.Errorf("version = %q, want %q", entries[0].Version, "1.2.0")
	}

	if !strings.Contains(entries[0].Body, "[added] New feature") {
		t.Errorf("expected [added] prefix, got %q", entries[0].Body)
	}
	if !strings.Contains(entries[0].Body, "[fixed] Bug fix") {
		t.Errorf("expected [fixed] prefix, got %q", entries[0].Body)
	}
	if !strings.Contains(entries[0].Body, "- Other change") {
		t.Errorf("expected plain prefix for empty kind, got %q", entries[0].Body)
	}

	if sourceURL == "" {
		t.Error("expected non-empty sourceURL")
	}
}

func TestArtifactHubFetcher_Fetch_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"9.9.9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestArtifactHubFetcher_Fetch_NoChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(artifactHubPackage{
			Version: "1.0.0",
			Changes: []artifactHubChange{},
		})
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty changes, got %d", len(entries))
	}
}

func TestArtifactHubFetcher_Fetch_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on invalid JSON, got %d", len(entries))
	}
}

func TestArtifactHubFetcher_Fetch_ConnectionError(t *testing.T) {
	client := newRewriteClient("http://127.0.0.1:1", "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on connection error, got %d", len(entries))
	}
}

func TestArtifactHubFetcher_Fetch_SecondPatternMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only respond to the second pattern (helm/owner/repo)
		if r.URL.Path == "/api/v1/packages/helm/myorg/myrepo/1.0.0" {
			_ = json.NewEncoder(w).Encode(artifactHubPackage{
				Version: "1.0.0",
				Changes: []artifactHubChange{
					{Kind: "added", Description: "Feature"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, sourceURL, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry from second pattern, got %d", len(entries))
	}
	if sourceURL == "" {
		t.Error("expected non-empty sourceURL")
	}
}

func TestArtifactHubFetcher_Fetch_MultipleVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/packages/helm/myrepo/myrepo/1.0.0":
			_ = json.NewEncoder(w).Encode(artifactHubPackage{
				Version: "1.0.0",
				Changes: []artifactHubChange{{Kind: "added", Description: "First"}},
			})
		case "/api/v1/packages/helm/myrepo/myrepo/1.1.0":
			_ = json.NewEncoder(w).Encode(artifactHubPackage{
				Version: "1.1.0",
				Changes: []artifactHubChange{{Kind: "fixed", Description: "Second"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://artifacthub.io")
	f := NewArtifactHubFetcher(client)

	entries, sourceURL, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0", "1.1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// sourceURL should be set from the first successful entry
	if sourceURL == "" {
		t.Error("expected non-empty sourceURL")
	}
}
