package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vertrost/argoiax/pkg/manifest"
)

func TestOCIRegistry_ListVersions(t *testing.T) {
	// Simulate an OCI registry that responds to /v2/<repo>/tags/list
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			// OCI ping endpoint
			w.WriteHeader(http.StatusOK)
		case "/v2/myorg/mychart/tags/list":
			resp := map[string]any{
				"name": "myorg/mychart",
				"tags": []string{"1.0.0", "1.1.0", "2.0.0"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	reg := NewOCIRegistry()
	ref := &manifest.ChartReference{
		RepoURL: fmt.Sprintf("oci://%s/myorg/mychart", server.Listener.Addr().String()),
		Type:    manifest.SourceTypeOCI,
	}

	versions, err := reg.ListVersions(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"1.0.0", "1.1.0", "2.0.0"}
	if len(versions) != len(expected) {
		t.Fatalf("expected %d versions, got %d", len(expected), len(versions))
	}
	for i, v := range versions {
		if v != expected[i] {
			t.Errorf("version[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestOCIRegistry_ListVersions_PrivateRegistry(t *testing.T) {
	const (
		username = "testuser"
		password = "testpass"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/myorg/privatechart/tags/list":
			resp := map[string]any{
				"name": "myorg/privatechart",
				"tags": []string{"1.0.0", "1.2.0"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create temporary Docker config with credentials for the test server.
	dockerCfgDir := t.TempDir()
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	configJSON := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, server.Listener.Addr().String(), auth)
	if err := os.WriteFile(filepath.Join(dockerCfgDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dockerCfgDir)

	reg := NewOCIRegistry()
	ref := &manifest.ChartReference{
		RepoURL: fmt.Sprintf("oci://%s/myorg/privatechart", server.Listener.Addr().String()),
		Type:    manifest.SourceTypeOCI,
	}

	versions, err := reg.ListVersions(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"1.0.0", "1.2.0"}
	if len(versions) != len(expected) {
		t.Fatalf("expected %d versions, got %d", len(expected), len(versions))
	}
	for i, v := range versions {
		if v != expected[i] {
			t.Errorf("version[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestOCIRegistry_ListVersions_Error(t *testing.T) {
	// Server that always returns 500 to trigger an error from crane.ListTags
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	reg := NewOCIRegistry()
	ref := &manifest.ChartReference{
		RepoURL: fmt.Sprintf("oci://%s/myorg/mychart", server.Listener.Addr().String()),
		Type:    manifest.SourceTypeOCI,
	}

	_, err := reg.ListVersions(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error from OCI registry failure")
	}
}
