package testutil

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v68/github"
)

// NewMockGitHubClient creates a mock GitHub API server that dispatches
// requests to the provided handlers keyed by "METHOD /path". Unmatched
// requests return 404.
func NewMockGitHubClient(t *testing.T, handlers map[string]http.HandlerFunc) *github.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := handlers[key]; ok {
			h(w, r)
			return
		}
		t.Logf("unhandled request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing mock server URL: %v", err)
	}
	client.BaseURL = baseURL
	return client
}
