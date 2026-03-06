package manifest

// SourceType represents the type of Helm chart source.
type SourceType int

// Source type constants.
const (
	SourceTypeHTTP SourceType = iota
	SourceTypeOCI
	SourceTypeGit
)

func (s SourceType) String() string {
	switch s {
	case SourceTypeHTTP:
		return "http"
	case SourceTypeOCI:
		return "oci"
	case SourceTypeGit:
		return "git"
	default:
		return "unknown"
	}
}

// ChartReference represents a single Helm chart pinned in an ArgoCD manifest.
type ChartReference struct {
	// Chart name (empty for OCI where the name is in the URL)
	ChartName string
	// Repository URL
	RepoURL string
	// Current pinned version
	TargetRevision string
	// Source type (HTTP, OCI, Git)
	Type SourceType
	// File path where this reference was found
	FilePath string
	// YAML path within the file for targeted updates (e.g., "spec.source.targetRevision")
	YAMLPath string
	// Index within sources array (-1 for single source)
	SourceIndex int
	// Whether this is inside an ApplicationSet template
	IsApplicationSet bool
}
