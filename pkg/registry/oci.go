package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/szhekpisov/argoiax/pkg/manifest"
)

// OCIRegistry implements Registry for OCI-based Helm registries.
type OCIRegistry struct{}

// NewOCIRegistry creates a new OCIRegistry.
func NewOCIRegistry() *OCIRegistry {
	return &OCIRegistry{}
}

// ListVersions returns all available tags for a chart from an OCI registry.
func (r *OCIRegistry) ListVersions(ctx context.Context, ref *manifest.ChartReference) ([]string, error) {
	// Strip oci:// prefix to get the repository reference
	repoRef := strings.TrimPrefix(ref.RepoURL, "oci://")

	slog.Debug("listing OCI tags", "repo", repoRef)

	opts := []crane.Option{
		crane.WithAuthFromKeychain(authn.DefaultKeychain),
		crane.WithContext(ctx),
	}

	tags, err := crane.ListTags(repoRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("listing OCI tags for %s: %w", repoRef, err)
	}

	return tags, nil
}
