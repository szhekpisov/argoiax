package registry

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vertrost/argoiax/pkg/config"
)

func TestRetryTransport_RetriesOn502ThenSucceeds(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client := &http.Client{
		Transport: &RetryTransport{
			Base:       http.DefaultTransport,
			MaxRetries: 3,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // closed via DrainBody
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestRetryTransport_PassesThroughNonRetryable(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &http.Client{
		Transport: &RetryTransport{
			Base:       http.DefaultTransport,
			MaxRetries: 3,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // closed via DrainBody
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt, got %d", got)
	}
}

func TestRetryTransport_Respects429RetryAfter(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{
		Transport: &RetryTransport{
			Base:       http.DefaultTransport,
			MaxRetries: 3,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // closed via DrainBody
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestGetGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		ghToken     string
		want        string
	}{
		{"GITHUB_TOKEN set", "gh-token-123", "", "gh-token-123"},
		{"GH_TOKEN set", "", "gh-alt-token", "gh-alt-token"},
		{"both set GITHUB_TOKEN wins", "primary", "secondary", "primary"},
		{"neither set", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", tt.githubToken)
			t.Setenv("GH_TOKEN", tt.ghToken)
			if got := GetGitHubToken(); got != tt.want {
				t.Errorf("GetGitHubToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthTransport_NilBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{
		Transport: &AuthTransport{Base: nil, Token: "tok"},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx
	resp, err := client.Do(req)                                        //nolint:bodyclose
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestNewAuthenticatedClient_WithAuth(t *testing.T) {
	cfg := &config.Config{
		Auth: config.Auth{
			HelmRepos: []config.HelmRepoAuth{
				{URL: "https://private.example.com", Username: "user", Password: "pass"},
			},
		},
	}

	client := NewAuthenticatedClient(cfg, "https://private.example.com")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestRetryAfterOrBackoff(t *testing.T) {
	// With valid Retry-After header on 429
	resp429 := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"5"}},
	}
	if got := retryAfterOrBackoff(resp429, 0); got != 5*time.Second {
		t.Errorf("expected 5s, got %v", got)
	}

	// Without Retry-After header on 429
	resp429NoHeader := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
	}
	if got := retryAfterOrBackoff(resp429NoHeader, 1); got != 2*time.Second {
		t.Errorf("expected 2s backoff, got %v", got)
	}

	// Invalid Retry-After header
	resp429Invalid := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"not-a-number"}},
	}
	if got := retryAfterOrBackoff(resp429Invalid, 0); got != 1*time.Second {
		t.Errorf("expected 1s backoff, got %v", got)
	}

	// Non-429 status
	resp500 := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Retry-After": []string{"5"}},
	}
	if got := retryAfterOrBackoff(resp500, 0); got != 1*time.Second {
		t.Errorf("expected 1s backoff for non-429, got %v", got)
	}
}

func TestRetryTransport_ExhaustsRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &http.Client{
		Transport: &RetryTransport{
			Base:       http.DefaultTransport,
			MaxRetries: 3,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // closed via DrainBody
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after exhausting retries, got %d", resp.StatusCode)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}
