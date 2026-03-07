package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vertrost/argoiax/pkg/config"
	"github.com/vertrost/argoiax/pkg/manifest"
	"github.com/vertrost/argoiax/pkg/semver"
)

func TestHelmHTTPRegistry_ListVersions(t *testing.T) {
	indexYAML := `
apiVersion: v1
entries:
  cert-manager:
    - version: 1.14.1
    - version: 1.13.2
    - version: 1.12.0
    - version: 1.11.0
  other-chart:
    - version: 2.0.0
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(indexYAML))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := manifest.ChartReference{
		ChartName: "cert-manager",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	versions, err := reg.ListVersions(context.Background(), &ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 4 {
		t.Fatalf("expected 4 versions, got %d", len(versions))
	}
}

func TestLatestStable_SkipsPreRelease(t *testing.T) {
	indexYAML := `
apiVersion: v1
entries:
  cert-manager:
    - version: 1.14.1
    - version: 1.13.2
    - version: 1.12.0
    - version: 2.0.0-beta.1
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(indexYAML))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := manifest.ChartReference{
		ChartName: "cert-manager",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	versions, err := reg.ListVersions(context.Background(), &ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	latest, err := semver.LatestStable(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should skip pre-release
	if latest != "1.14.1" {
		t.Errorf("expected 1.14.1, got %s", latest)
	}
}

func TestLatestStable_WithConstraint(t *testing.T) {
	indexYAML := `
apiVersion: v1
entries:
  cert-manager:
    - version: 1.14.1
    - version: 1.13.2
    - version: 2.0.0
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(indexYAML))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := manifest.ChartReference{
		ChartName: "cert-manager",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	versions, err := reg.ListVersions(context.Background(), &ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	latest, err := semver.LatestStable(versions, ">=1.0.0, <2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if latest != "1.14.1" {
		t.Errorf("expected 1.14.1 (constrained), got %s", latest)
	}
}

func TestHelmHTTPRegistry_ChartNotFound(t *testing.T) {
	indexYAML := `
apiVersion: v1
entries:
  other-chart:
    - version: 1.0.0
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(indexYAML))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := manifest.ChartReference{
		ChartName: "nonexistent",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	_, err := reg.ListVersions(context.Background(), &ref)
	if err == nil {
		t.Error("expected error for nonexistent chart")
	}
}

func TestHelmHTTPRegistry_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := &manifest.ChartReference{
		ChartName: "mychart",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("expected error to mention status, got: %v", err)
	}
}

func TestHelmHTTPRegistry_InvalidYAML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{{invalid yaml"))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := &manifest.ChartReference{
		ChartName: "mychart",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing index.yaml") {
		t.Errorf("expected error to mention parsing, got: %v", err)
	}
}

func TestHelmHTTPRegistry_CachesIndex(t *testing.T) {
	var callCount atomic.Int32

	indexYAML := `
apiVersion: v1
entries:
  mychart:
    - version: 1.0.0
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		_, _ = w.Write([]byte(indexYAML))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	ref := &manifest.ChartReference{
		ChartName: "mychart",
		RepoURL:   server.URL,
		Type:      manifest.SourceTypeHTTP,
	}

	// First call fetches from server
	v1, err := reg.ListVersions(context.Background(), ref)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(v1) != 1 {
		t.Fatalf("expected 1 version, got %d", len(v1))
	}

	// Second call should use cache, not hit server again
	v2, err := reg.ListVersions(context.Background(), ref)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(v2) != 1 {
		t.Fatalf("expected 1 version, got %d", len(v2))
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("expected server to be called once (cached), got %d", got)
	}
}

func TestHelmHTTPRegistry_CachesClient(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := NewHelmHTTPRegistry(cfg)

	repoURL := "https://example.com/charts"
	c1 := reg.getClient(repoURL)
	c2 := reg.getClient(repoURL)

	if c1 != c2 {
		t.Error("expected same client instance from cache")
	}
}
