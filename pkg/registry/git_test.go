package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vertrost/argoiax/pkg/manifest"
)

func TestGitRegistry_ListVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tags := []gitTag{
			{Name: "v1.0.0"},
			{Name: "v1.1.0"},
			{Name: "v2.0.0"},
		}
		_ = json.NewEncoder(w).Encode(tags)
	}))
	defer server.Close()

	// Create a GitRegistry with a client that rewrites GitHub API URLs to our test server
	reg := &GitRegistry{
		client: newRewriteClient(server.URL),
	}

	ref := &manifest.ChartReference{
		RepoURL: "https://github.com/myorg/myrepo",
		Type:    manifest.SourceTypeGit,
	}

	versions, err := reg.ListVersions(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	expected := []string{"v1.0.0", "v1.1.0", "v2.0.0"}
	for i, v := range versions {
		if v != expected[i] {
			t.Errorf("version[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestGitRegistry_ListVersions_Paginated(t *testing.T) {
	callCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Link", `<`+serverURL+`/repos/myorg/myrepo/tags?page=2>; rel="next"`)
			tags := []gitTag{{Name: "v1.0.0"}}
			_ = json.NewEncoder(w).Encode(tags)
			return
		}
		tags := []gitTag{{Name: "v2.0.0"}}
		_ = json.NewEncoder(w).Encode(tags)
	}))
	defer server.Close()
	serverURL = server.URL

	reg := &GitRegistry{
		client: newRewriteClient(server.URL),
	}

	ref := &manifest.ChartReference{
		RepoURL: "https://github.com/myorg/myrepo",
		Type:    manifest.SourceTypeGit,
	}

	versions, err := reg.ListVersions(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
}

func TestGitRegistry_ListVersions_ConnectionError(t *testing.T) {
	reg := &GitRegistry{client: newRewriteClient("http://127.0.0.1:1")}

	ref := &manifest.ChartReference{
		RepoURL: "https://github.com/myorg/myrepo",
		Type:    manifest.SourceTypeGit,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestGitRegistry_ListVersions_EmptyOwnerRepo(t *testing.T) {
	reg := &GitRegistry{client: &http.Client{}}

	ref := &manifest.ChartReference{
		RepoURL: "no-slash-url",
		Type:    manifest.SourceTypeGit,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Error("expected error for URL producing empty owner/repo")
	}
}

func TestGitRegistry_ListVersions_InvalidURL(t *testing.T) {
	reg := &GitRegistry{
		client: &http.Client{},
	}

	ref := &manifest.ChartReference{
		RepoURL: "https://not-github.com/something",
		Type:    manifest.SourceTypeGit,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Error("expected error for non-GitHub URL")
	}
}

func TestGitRegistry_ListVersions_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	reg := &GitRegistry{
		client: newRewriteClient(server.URL),
	}

	ref := &manifest.ChartReference{
		RepoURL: "https://github.com/myorg/myrepo",
		Type:    manifest.SourceTypeGit,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Error("expected error on server error response")
	}
}

func TestGitRegistry_ListVersions_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	reg := &GitRegistry{
		client: newRewriteClient(server.URL),
	}

	ref := &manifest.ChartReference{
		RepoURL: "https://github.com/myorg/myrepo",
		Type:    manifest.SourceTypeGit,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

func TestParseNextLink(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"empty", "", ""},
		{"no next", `<https://api.github.com/repos/x/y/tags?page=1>; rel="prev"`, ""},
		{"has next", `<https://api.github.com/repos/x/y/tags?page=2>; rel="next"`, "https://api.github.com/repos/x/y/tags?page=2"},
		{
			"multiple links",
			`<https://api.github.com/repos/x/y/tags?page=1>; rel="prev", <https://api.github.com/repos/x/y/tags?page=3>; rel="next"`,
			"https://api.github.com/repos/x/y/tags?page=3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNextLink(tt.header)
			if got != tt.want {
				t.Errorf("parseNextLink(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestExtractGitHubOwnerRepo(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
	}{
		{"https://github.com/myorg/myrepo.git", "myorg", "myrepo"},
		{"https://github.com/myorg/myrepo", "myorg", "myrepo"},
		{"http://github.com/myorg/myrepo", "myorg", "myrepo"},
		{"git@github.com:myorg/myrepo.git", "myorg", "myrepo"},
		{"github.com/myorg/myrepo", "myorg", "myrepo"},
		{"https://github.com/myorg/myrepo/tree/main", "myorg", "myrepo"},
		{"not-a-github-url", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo := ExtractGitHubOwnerRepo(tt.url)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("ExtractGitHubOwnerRepo(%q) = (%q, %q), want (%q, %q)",
					tt.url, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// newRewriteClient creates an HTTP client that rewrites all requests to the given base URL.
func newRewriteClient(base string) *http.Client {
	return &http.Client{
		Transport: &rewriteTransport{
			base:   http.DefaultTransport,
			target: base,
		},
	}
}

type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.target + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body) //nolint:gosec // test-only URL rewrite
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return t.base.RoundTrip(newReq)
}
