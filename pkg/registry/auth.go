package registry

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/vertrost/ancaeus/pkg/config"
)

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
	if t.Username != "" || t.Password != "" {
		req.SetBasicAuth(t.Username, t.Password)
	} else if t.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.Token))
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// NewAuthenticatedClient creates an HTTP client with authentication for a given repo URL.
func NewAuthenticatedClient(cfg *config.Config, repoURL string) *http.Client {
	auth := cfg.FindRepoAuth(repoURL)
	if auth == nil {
		return &http.Client{}
	}

	return &http.Client{
		Transport: &AuthTransport{
			Username: auth.Username,
			Password: auth.Password,
		},
	}
}

// NewTokenClient creates an HTTP client that uses Bearer token authentication.
func NewTokenClient(token string) *http.Client {
	return &http.Client{
		Transport: &AuthTransport{Token: token},
	}
}

// GetGitHubToken returns the GitHub token from environment variables.
func GetGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}
