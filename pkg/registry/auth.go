package registry

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/vertrost/argoiax/pkg/config"
)

const defaultHTTPTimeout = 60 * time.Second

// DrainBody reads any remaining data from body and closes it.
// Use as: defer registry.DrainBody(resp.Body)
func DrainBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// AuthTransport adds authentication headers to HTTP requests.
type AuthTransport struct {
	Base     http.RoundTripper
	Username string
	Password string
	Token    string
}

// RoundTrip adds authentication headers and delegates to the base transport.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if t.Username != "" || t.Password != "" {
		r.SetBasicAuth(t.Username, t.Password)
	} else if t.Token != "" {
		r.Header.Set("Authorization", "Bearer "+t.Token)
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// RetryTransport wraps an http.RoundTripper and retries on transient errors with exponential backoff.
type RetryTransport struct {
	Base       http.RoundTripper
	MaxRetries int
}

var retryableStatusCodes = map[int]bool{
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
}

// RoundTrip executes the request with retry logic for transient failures.
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	maxRetries := t.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var resp *http.Response
	var err error

	for attempt := range maxRetries {
		resp, err = base.RoundTrip(req.Clone(req.Context()))

		if err != nil && attempt == maxRetries-1 {
			return nil, err
		}
		if err != nil {
			if waitErr := retryWait(req.Context(), backoffDuration(attempt)); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if !retryableStatusCodes[resp.StatusCode] {
			return resp, nil
		}
		if attempt == maxRetries-1 {
			break
		}

		wait := retryAfterOrBackoff(resp, attempt)
		DrainBody(resp.Body)
		if waitErr := retryWait(req.Context(), wait); waitErr != nil {
			return nil, waitErr
		}
	}

	return resp, nil
}

var backoffSchedule = [...]time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

func backoffDuration(attempt int) time.Duration {
	if attempt < 0 || attempt >= len(backoffSchedule) {
		return backoffSchedule[len(backoffSchedule)-1]
	}
	return backoffSchedule[attempt]
}

func retryAfterOrBackoff(resp *http.Response, attempt int) time.Duration {
	if resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, err := strconv.Atoi(ra); err == nil && seconds > 0 && seconds <= 60 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return backoffDuration(attempt)
}

func retryWait(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// wrapWithRetry wraps a transport with retry logic.
func wrapWithRetry(base http.RoundTripper) *RetryTransport {
	return &RetryTransport{Base: base, MaxRetries: 3}
}

// NewAuthenticatedClient creates an HTTP client with authentication for a given repo URL.
func NewAuthenticatedClient(cfg *config.Config, repoURL string) *http.Client {
	auth := cfg.FindRepoAuth(repoURL)
	if auth == nil {
		return &http.Client{
			Transport: wrapWithRetry(http.DefaultTransport),
			Timeout:   defaultHTTPTimeout,
		}
	}

	return &http.Client{
		Transport: wrapWithRetry(&AuthTransport{
			Username: auth.Username,
			Password: auth.Password,
		}),
		Timeout: defaultHTTPTimeout,
	}
}

// NewTokenClient creates an HTTP client that uses Bearer token authentication.
func NewTokenClient(token string) *http.Client {
	return &http.Client{
		Transport: wrapWithRetry(&AuthTransport{Token: token}),
		Timeout:   defaultHTTPTimeout,
	}
}

// GetGitHubToken returns the GitHub token from environment variables.
func GetGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}
