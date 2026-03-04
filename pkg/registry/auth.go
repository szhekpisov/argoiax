package registry

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/vertrost/argoiax/pkg/config"
)

const defaultHTTPTimeout = 60 * time.Second

// DrainBody reads any remaining data from body and closes it.
// Use as: defer registry.DrainBody(resp.Body)
func DrainBody(body io.ReadCloser) {
	io.Copy(io.Discard, body)
	body.Close()
}

// AuthTransport adds authentication headers to HTTP requests.
type AuthTransport struct {
	Base     http.RoundTripper
	Username string
	Password string
	Token    string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if t.Username != "" || t.Password != "" {
		r.SetBasicAuth(t.Username, t.Password)
	} else if t.Token != "" {
		r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.Token))
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// NewAuthenticatedClient creates an HTTP client with authentication for a given repo URL.
func NewAuthenticatedClient(cfg *config.Config, repoURL string) *http.Client {
	auth := cfg.FindRepoAuth(repoURL)
	if auth == nil {
		return &http.Client{Timeout: defaultHTTPTimeout}
	}

	return &http.Client{
		Transport: &AuthTransport{
			Username: auth.Username,
			Password: auth.Password,
		},
		Timeout: defaultHTTPTimeout,
	}
}

// NewTokenClient creates an HTTP client that uses Bearer token authentication.
func NewTokenClient(token string) *http.Client {
	return &http.Client{
		Transport: &AuthTransport{Token: token},
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
