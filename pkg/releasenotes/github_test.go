package releasenotes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubFetcher_Name(t *testing.T) {
	f := NewGitHubFetcher(nil)
	if got := f.Name(); got != "github-releases" {
		t.Errorf("Name() = %q, want %q", got, "github-releases")
	}
}

func TestGitHubFetcher_Fetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/myorg/myrepo/releases/tags/1.2.0":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "1.2.0",
				Body:    "release notes for 1.2.0",
				HTMLURL: "https://github.com/myorg/myrepo/releases/tag/1.2.0",
			})
		case "/repos/myorg/myrepo/releases/tags/v1.3.0":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v1.3.0",
				Body:    "release notes for 1.3.0",
				HTMLURL: "https://github.com/myorg/myrepo/releases/tag/v1.3.0",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Override the GitHub API URL by using a custom client that rewrites URLs
	client := newRewriteClient(server.URL, "https://api.github.com")

	f := NewGitHubFetcher(client)
	entries, sourceURL, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.2.0", "1.3.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sourceURL != "https://github.com/myorg/myrepo/releases" {
		t.Errorf("unexpected sourceURL: %s", sourceURL)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Version != "1.2.0" {
		t.Errorf("entries[0].Version = %q, want %q", entries[0].Version, "1.2.0")
	}
	if entries[0].Body != "release notes for 1.2.0" {
		t.Errorf("entries[0].Body = %q", entries[0].Body)
	}
}

func TestGitHubFetcher_Fetch_ChartNameTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only respond to the chart-name tag pattern
		if r.URL.Path == "/repos/prometheus-community/helm-charts/releases/tags/kube-prometheus-stack-70.4.1" {
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "kube-prometheus-stack-70.4.1",
				Body:    "release notes for kube-prometheus-stack 70.4.1",
				HTMLURL: "https://github.com/prometheus-community/helm-charts/releases/tag/kube-prometheus-stack-70.4.1",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{
		Owner:     "prometheus-community",
		Repo:      "helm-charts",
		ChartName: "kube-prometheus-stack",
	}, []string{"70.4.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry via chart-name tag pattern, got %d", len(entries))
	}
	if entries[0].Version != "70.4.1" {
		t.Errorf("entries[0].Version = %q, want %q", entries[0].Version, "70.4.1")
	}
}

func TestGitHubFetcher_Fetch_ChartSuffixedTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only respond to the {chartName}-chart-{version} pattern
		if r.URL.Path == "/repos/kubernetes/autoscaler/releases/tags/cluster-autoscaler-chart-9.46.0" {
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "cluster-autoscaler-chart-9.46.0",
				Body:    "## What's Changed\n* Bump cluster-autoscaler to 1.32.0",
				HTMLURL: "https://github.com/kubernetes/autoscaler/releases/tag/cluster-autoscaler-chart-9.46.0",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{
		Owner:     "kubernetes",
		Repo:      "autoscaler",
		ChartName: "cluster-autoscaler",
	}, []string{"9.46.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry via chart-suffixed tag pattern, got %d", len(entries))
	}
	if entries[0].Version != "9.46.0" {
		t.Errorf("Version = %q, want %q", entries[0].Version, "9.46.0")
	}
	if entries[0].Body != "## What's Changed\n* Bump cluster-autoscaler to 1.32.0" {
		t.Errorf("Body = %q", entries[0].Body)
	}
}

func TestGitHubFetcher_Fetch_HelmVTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/bitnami-labs/sealed-secrets/releases/tags/helm-v2.18.3" {
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "helm-v2.18.3",
				Body:    "## What's Changed\n* Update sealed-secrets to 0.29.0",
				HTMLURL: "https://github.com/bitnami-labs/sealed-secrets/releases/tag/helm-v2.18.3",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{
		Owner:     "bitnami-labs",
		Repo:      "sealed-secrets",
		ChartName: "sealed-secrets",
	}, []string{"2.18.3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry via helm-v tag pattern, got %d", len(entries))
	}
	if entries[0].Version != "2.18.3" {
		t.Errorf("Version = %q, want %q", entries[0].Version, "2.18.3")
	}
}

func TestGitHubFetcher_Fetch_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"9.9.9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for missing release, got %d", len(entries))
	}
}

func TestGitHubFetcher_Fetch_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on server error, got %d", len(entries))
	}
}

func TestGitHubFetcher_Fetch_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	client := newRewriteClient(server.URL, "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// JSON decode error is treated as a fetch failure, so no entries
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on invalid JSON, got %d", len(entries))
	}
}

func TestGitHubFetcher_Fetch_NoRepoMapping(t *testing.T) {
	f := NewGitHubFetcher(nil)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{ChartName: "datadog"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty Owner/Repo, got %d", len(entries))
	}
}

func TestGitHubFetcher_Fetch_ConnectionError(t *testing.T) {
	// Use a client pointing at an invalid URL to force a transport error
	client := newRewriteClient("http://127.0.0.1:1", "https://api.github.com")
	f := NewGitHubFetcher(client)
	entries, _, err := f.Fetch(context.Background(), GitHubRepo{Owner: "myorg", Repo: "myrepo"}, []string{"1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on connection error, got %d", len(entries))
	}
}

// newRewriteClient creates an HTTP client that rewrites requests from oldBase to newBase.
func newRewriteClient(newBase, oldBase string) *http.Client {
	return &http.Client{
		Transport: &rewriteTransport{
			base:    http.DefaultTransport,
			oldBase: oldBase,
			newBase: newBase,
		},
	}
}

type rewriteTransport struct {
	base    http.RoundTripper
	oldBase string
	newBase string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.newBase + req.URL.Path
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
