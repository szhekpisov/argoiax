package registry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // drained below
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

// errTransport is a RoundTripper that returns an error for the first N calls,
// then delegates to a real server.
type errTransport struct {
	failCount int
	called    atomic.Int32
	delegate  http.RoundTripper
}

func (t *errTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := int(t.called.Add(1))
	if n <= t.failCount {
		return nil, errors.New("connection refused")
	}
	return t.delegate.RoundTrip(req)
}

func TestRetryTransport_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	et := &errTransport{
		failCount: 2,
		delegate:  http.DefaultTransport,
	}

	client := &http.Client{
		Transport: &RetryTransport{
			Base:       et,
			MaxRetries: 3,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // drained below
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := et.called.Load(); got != 3 {
		t.Errorf("expected 3 calls to transport, got %d", got)
	}
}

func TestRetryWait_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so retryWait returns ctx.Err()
	cancel()

	err := retryWait(ctx, 10*time.Second)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRetryTransport_NilBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// RetryTransport with nil Base should use http.DefaultTransport
	rt := &RetryTransport{
		Base:       nil,
		MaxRetries: 1,
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := rt.RoundTrip(req)                                     //nolint:bodyclose // drained below
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRetryTransport_DefaultMaxRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	// MaxRetries <= 0 should default to 3
	client := &http.Client{
		Transport: &RetryTransport{
			Base:       http.DefaultTransport,
			MaxRetries: 0,
		},
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := client.Do(req)                                        //nolint:bodyclose // drained below
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer DrainBody(resp.Body)

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts (default), got %d", got)
	}
}

func TestRetryAfterOrBackoff_TooLargeValue(t *testing.T) {
	// Retry-After value > 60 should be ignored, falling back to backoff
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"120"}},
	}
	got := retryAfterOrBackoff(resp, 0)
	if got != 1*time.Second {
		t.Errorf("expected 1s backoff (Retry-After > 60 ignored), got %v", got)
	}
}

func TestDrainBody(t *testing.T) {
	body := io.NopCloser(strings.NewReader("hello world"))
	// Should not panic and should drain + close
	DrainBody(body)
}

func TestWrapWithRetry(t *testing.T) {
	rt := wrapWithRetry(http.DefaultTransport)
	if rt == nil {
		t.Fatal("expected non-nil RetryTransport")
	}
	if rt.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", rt.MaxRetries)
	}
	if rt.Base != http.DefaultTransport {
		t.Error("expected Base to be http.DefaultTransport")
	}
}

func TestRetryTransport_NetworkError_ExhaustsRetries(t *testing.T) {
	// Transport always fails -- after MaxRetries the last error should be returned.
	alwaysFail := &errTransport{
		failCount: 100,
		delegate:  http.DefaultTransport,
	}

	rt := &RetryTransport{
		Base:       alwaysFail,
		MaxRetries: 2,
	}

	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1", http.NoBody) //nolint:noctx // test only
	_, err := rt.RoundTrip(req)                                                  //nolint:bodyclose // error path, no body
	if err == nil {
		t.Fatal("expected error after exhausting retries on network errors")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected connection refused error, got: %v", err)
	}
	if got := alwaysFail.called.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestRetryTransport_ContextCancelDuring5xxRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel the context after a short delay so retryWait returns context.Canceled
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	rt := &RetryTransport{
		Base:       http.DefaultTransport,
		MaxRetries: 5,
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
	_, err := rt.RoundTrip(req) //nolint:bodyclose // error path, no body
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestRetryTransport_ContextCancelDuringNetworkRetry(t *testing.T) {
	// Transport always fails, and context is cancelled during wait
	alwaysFail := &errTransport{
		failCount: 100,
		delegate:  http.DefaultTransport,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	rt := &RetryTransport{
		Base:       alwaysFail,
		MaxRetries: 5,
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1", http.NoBody)
	_, err := rt.RoundTrip(req) //nolint:bodyclose // error path, no body
	if err == nil {
		t.Fatal("expected error from context cancellation during network error retry")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
