package pr

import (
	"context"

	"github.com/vertrost/ancaeus/pkg/releasenotes"
)

// UpdateInfo contains all information needed to create a PR for a chart update.
type UpdateInfo struct {
	ChartName       string
	OldVersion      string
	NewVersion      string
	FilePath        string
	RepoURL         string
	IsBreaking      bool
	BreakingReasons []string
	ReleaseNotes    *releasenotes.Notes
}

// PRResult contains the result of creating a PR.
type PRResult struct {
	PRURL    string
	PRNumber int
	Branch   string
}

// Creator is the interface for creating pull requests.
type Creator interface {
	// CreatePR creates a pull request for a chart update.
	CreatePR(ctx context.Context, info UpdateInfo, fileContent []byte, baseBranch string) (*PRResult, error)

	// ExistingPR checks if a PR already exists for this update.
	ExistingPR(ctx context.Context, branch string) (bool, error)
}
