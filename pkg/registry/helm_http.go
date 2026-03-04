package registry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/vertrost/ancaeus/pkg/config"
	"github.com/vertrost/ancaeus/pkg/manifest"
	"golang.org/x/sync/singleflight"
	"gopkg.in/yaml.v3"
)

// HelmHTTPRegistry implements Registry for classic Helm HTTP repositories.
type HelmHTTPRegistry struct {
	cfg     *config.Config
	cache   sync.Map            // repoURL -> *indexCache
	clients sync.Map            // repoURL -> *http.Client
	group   singleflight.Group  // dedup concurrent fetches for the same repo
}

type indexCache struct {
	entries map[string][]string // chartName -> versions
}

// indexFile represents a simplified Helm repo index.yaml structure.
type indexFile struct {
	Entries map[string][]indexEntry `yaml:"entries"`
}

type indexEntry struct {
	Version string `yaml:"version"`
}

// NewHelmHTTPRegistry creates a new HelmHTTPRegistry.
func NewHelmHTTPRegistry(cfg *config.Config) *HelmHTTPRegistry {
	return &HelmHTTPRegistry{cfg: cfg}
}

func (r *HelmHTTPRegistry) ListVersions(ctx context.Context, ref manifest.ChartReference) ([]string, error) {
	idx, err := r.fetchIndex(ctx, ref.RepoURL)
	if err != nil {
		return nil, err
	}

	versions, ok := idx.entries[ref.ChartName]
	if !ok {
		return nil, fmt.Errorf("chart %q not found in repo %s", ref.ChartName, ref.RepoURL)
	}

	return slices.Clone(versions), nil
}

func (r *HelmHTTPRegistry) fetchIndex(ctx context.Context, repoURL string) (*indexCache, error) {
	if cached, ok := r.cache.Load(repoURL); ok {
		return cached.(*indexCache), nil
	}

	// Use a context detached from the caller so that if the winning goroutine's
	// context is cancelled, other waiters sharing this singleflight call are not affected.
	v, err, _ := r.group.Do(repoURL, func() (interface{}, error) {
		// Double-check after winning the race
		if cached, ok := r.cache.Load(repoURL); ok {
			return cached.(*indexCache), nil
		}
		return r.doFetchIndex(context.WithoutCancel(ctx), repoURL)
	})
	if err != nil {
		return nil, err
	}
	return v.(*indexCache), nil
}

func (r *HelmHTTPRegistry) doFetchIndex(ctx context.Context, repoURL string) (*indexCache, error) {
	indexURL := strings.TrimSuffix(repoURL, "/") + "/index.yaml"
	slog.Debug("fetching helm index", "url", indexURL)

	client := r.getClient(repoURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching index from %s: %w", indexURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching index from %s: status %d", indexURL, resp.StatusCode)
	}

	const maxIndexSize = 50 * 1024 * 1024 // 50 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexSize))
	if err != nil {
		return nil, fmt.Errorf("reading index body: %w", err)
	}

	var idx indexFile
	if err := yaml.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parsing index.yaml from %s: %w", repoURL, err)
	}

	cache := &indexCache{
		entries: make(map[string][]string),
	}
	for name, entries := range idx.Entries {
		versions := make([]string, 0, len(entries))
		for _, e := range entries {
			versions = append(versions, e.Version)
		}
		cache.entries[name] = versions
	}

	r.cache.Store(repoURL, cache)
	return cache, nil
}

func (r *HelmHTTPRegistry) getClient(repoURL string) *http.Client {
	if c, ok := r.clients.Load(repoURL); ok {
		return c.(*http.Client)
	}
	c := NewAuthenticatedClient(r.cfg, repoURL)
	actual, _ := r.clients.LoadOrStore(repoURL, c)
	return actual.(*http.Client)
}
