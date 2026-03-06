package registry

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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
