package manifest

import (
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
)

// Walker recursively walks directories to find YAML files containing ArgoCD manifests.
type Walker struct {
	IgnorePatterns []string
}

// Walk scans the given directories for YAML files and returns all ChartReferences found.
func (w *Walker) Walk(dirs []string) ([]ChartReference, error) {
	var allRefs []ChartReference

	for _, dir := range dirs {
		refs, err := w.walkDir(dir)
		if err != nil {
			return nil, err
		}
		allRefs = append(allRefs, refs...)
	}

	return allRefs, nil
}

func (w *Walker) walkDir(root string) ([]ChartReference, error) {
	var refs []ChartReference

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("error accessing path", "path", path, "error", err)
			return nil
		}

		if d.IsDir() {
			if w.shouldIgnore(path) {
				return filepath.SkipDir
			}
			return nil
		}

		if !isYAMLFile(path) {
			return nil
		}

		if w.shouldIgnore(path) {
			return nil
		}

		slog.Debug("scanning file", "path", path)

		fileRefs, err := ParseFile(path)
		if err != nil {
			slog.Warn("error parsing file", "path", path, "error", err)
			return nil
		}

		refs = append(refs, fileRefs...)
		return nil
	})

	return refs, err
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func (w *Walker) shouldIgnore(path string) bool {
	for _, pattern := range w.IgnorePatterns {
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
		// Also try matching against the base name
		matched, err = filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		// Try matching with ** patterns (simplified globstar)
		if strings.Contains(pattern, "**") {
			if matchesGlobstar(path, pattern) {
				return true
			}
		}
	}
	return false
}

// matchesGlobstar handles patterns containing "**" which match across directory
// boundaries (e.g., "**/foo", "dir/**/bar.yaml", "dir/**").
func matchesGlobstar(path, pattern string) bool {
	prefix, suffix, found := strings.Cut(pattern, "**/")
	if !found {
		// No "**/" found — pattern is "**" or ends with "**" (e.g., "dir/**").
		prefix := strings.TrimSuffix(pattern, "**")
		return prefix == "" || strings.HasPrefix(path, prefix)
	}

	if prefix != "" && !strings.HasPrefix(path, prefix) {
		return false
	}

	subPath := path
	if prefix != "" {
		subPath = path[len(prefix):]
	}

	return matchesDeepPattern(subPath, suffix)
}

func matchesDeepPattern(path, pattern string) bool {
	parts := strings.Split(path, string(filepath.Separator))
	for i := range parts {
		subPath := filepath.Join(parts[i:]...)
		matched, err := filepath.Match(pattern, subPath)
		if err == nil && matched {
			return true
		}
	}
	return false
}
