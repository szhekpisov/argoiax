package pr

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/vertrost/argoiax/pkg/releasenotes"
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

// Result contains the result of creating a PR.
type Result struct {
	PRURL    string
	PRNumber int
	Branch   string
}

// FileUpdate represents updated content for a single file.
type FileUpdate struct {
	FilePath    string
	FileContent []byte
}

// UpdateGroup represents a group of chart updates to be submitted as a single PR.
type UpdateGroup struct {
	Updates []UpdateInfo
	Files   []FileUpdate
}

// GroupTemplateData holds data for rendering group branch/title templates.
type GroupTemplateData struct {
	Count        int
	FileCount    int
	FilePath     string // set only for per-file (single file)
	FileBaseName string // basename without extension, or "batch"
	ChartNames   []string
	Summary      string // joined chart names
}

// Creator is the interface for creating pull requests.
type Creator interface {
	// CreatePR creates a pull request for a single chart update.
	CreatePR(ctx context.Context, info *UpdateInfo, fileContent []byte, baseBranch string) (*Result, error)

	// CreateGroupPR creates a pull request for a group of chart updates.
	CreateGroupPR(ctx context.Context, group UpdateGroup, baseBranch string) (*Result, error)

	// ExistingPR checks if a PR already exists for this update.
	ExistingPR(ctx context.Context, branch string) (bool, error)
}

// NewGroupTemplateData builds GroupTemplateData from an UpdateGroup.
func NewGroupTemplateData(group UpdateGroup) GroupTemplateData {
	names := make([]string, 0, len(group.Updates))
	seen := make(map[string]bool)
	for _, u := range group.Updates {
		if !seen[u.ChartName] {
			names = append(names, u.ChartName)
			seen[u.ChartName] = true
		}
	}

	fileBaseName := defaultBatchBaseName
	filePath := ""
	if len(group.Files) == 1 {
		filePath = group.Files[0].FilePath
		base := filepath.Base(filePath)
		fileBaseName = strings.TrimSuffix(base, filepath.Ext(base))
	}

	return GroupTemplateData{
		Count:        len(group.Updates),
		FileCount:    len(group.Files),
		FilePath:     filePath,
		FileBaseName: fileBaseName,
		ChartNames:   names,
		Summary:      strings.Join(names, ", "),
	}
}

const (
	// LabelBreakingChange is the label applied to PRs with breaking changes.
	LabelBreakingChange = "breaking-change"

	// defaultBatchBaseName is used as the FileBaseName when a group spans multiple files.
	defaultBatchBaseName = "batch"
)

var invalidBranchChars = regexp.MustCompile(`[~^:?*\[\]\\@{}\s]+`)
var consecutiveDashes = regexp.MustCompile(`-{2,}`)
var doubleDots = regexp.MustCompile(`\.\.+`)

// SanitizeBranchName cleans a string so it is a valid git ref name.
func SanitizeBranchName(name string) string {
	// Replace invalid git ref characters with dashes
	name = invalidBranchChars.ReplaceAllString(name, "-")
	// Replace ".." sequences (forbidden in git refs)
	name = doubleDots.ReplaceAllString(name, "-")
	// Collapse consecutive dashes
	name = consecutiveDashes.ReplaceAllString(name, "-")
	// Remove leading/trailing dashes and dots
	name = strings.Trim(name, "-.")
	// Truncate to reasonable length
	if len(name) > 200 {
		name = name[:200]
		name = strings.TrimRight(name, "-.")
	}
	return name
}

// RenderTemplate executes a Go text/template against the given data.
func RenderTemplate(tmpl string, data any) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}
