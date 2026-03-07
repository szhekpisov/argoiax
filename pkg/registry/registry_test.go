package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
)

func TestFactory_GetRegistry(t *testing.T) {
	tests := []struct {
		name    string
		refType manifest.SourceType
		wantErr bool
	}{
		{"HTTP", manifest.SourceTypeHTTP, false},
		{"OCI", manifest.SourceTypeOCI, false},
		{"Git", manifest.SourceTypeGit, false},
		{"Unknown", manifest.SourceType(99), true},
	}

	cfg := config.DefaultConfig()
	factory := NewFactory(cfg, "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := &manifest.ChartReference{Type: tt.refType}
			reg, err := factory.GetRegistry(ref)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if reg == nil {
				t.Fatal("expected non-nil registry")
			}
		})
	}
}

func TestAuthTransport_BasicAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &AuthTransport{
		Username: "user",
		Password: "pass",
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if gotAuth == "" {
		t.Error("expected Authorization header to be set")
	}
	// Basic auth header should be present
	if len(gotAuth) < 6 || gotAuth[:6] != "Basic " {
		t.Errorf("expected Basic auth, got %q", gotAuth)
	}
}

func TestAuthTransport_BearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &AuthTransport{
		Token: "my-token",
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if gotAuth != "Bearer my-token" {
		t.Errorf("expected Bearer auth, got %q", gotAuth)
	}
}

func TestAuthTransport_NoAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &AuthTransport{}

	req, _ := http.NewRequest(http.MethodGet, server.URL, http.NoBody) //nolint:noctx // test only
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if gotAuth != "" {
		t.Errorf("expected no auth header, got %q", gotAuth)
	}
}

func TestNewAuthenticatedClient_NoAuth(t *testing.T) {
	cfg := config.DefaultConfig()
	client := NewAuthenticatedClient(cfg, "https://example.com/charts")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewTokenClient(t *testing.T) {
	client := NewTokenClient("test-token")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		attempt int
		want    string
	}{
		{0, "1s"},
		{1, "2s"},
		{2, "4s"},
		{3, "4s"},  // beyond schedule, clamps to last
		{-1, "4s"}, // negative, clamps to last
	}

	for _, tt := range tests {
		got := backoffDuration(tt.attempt)
		if got.String() != tt.want {
			t.Errorf("backoffDuration(%d) = %v, want %s", tt.attempt, got, tt.want)
		}
	}
}
