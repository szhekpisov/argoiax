package registry

import (
	"context"
	"fmt"

	"github.com/vertrost/ancaeus/pkg/config"
	"github.com/vertrost/ancaeus/pkg/manifest"
)

// Registry is the interface for checking chart versions across different repository types.
type Registry interface {
	// ListVersions returns all available versions for a chart reference.
	ListVersions(ctx context.Context, ref manifest.ChartReference) ([]string, error)
}

// Factory creates Registry instances based on the chart reference type.
type Factory struct {
	helmHTTP *HelmHTTPRegistry
	oci      *OCIRegistry
	git      *GitRegistry
}

// NewFactory creates a new Registry factory with the given config.
func NewFactory(cfg *config.Config) *Factory {
	return &Factory{
		helmHTTP: NewHelmHTTPRegistry(cfg),
		oci:      NewOCIRegistry(cfg),
		git:      NewGitRegistry(),
	}
}

// GetRegistry returns the appropriate Registry implementation for the given chart reference.
func (f *Factory) GetRegistry(ref manifest.ChartReference) (Registry, error) {
	switch ref.Type {
	case manifest.SourceTypeHTTP:
		return f.helmHTTP, nil
	case manifest.SourceTypeOCI:
		return f.oci, nil
	case manifest.SourceTypeGit:
		return f.git, nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", ref.Type)
	}
}

