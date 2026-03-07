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
		if matchGlob(pattern, path) {
			return true
		}
	}
	return false
}

// matchGlob matches a glob pattern against a path, supporting ** for crossing
// directory boundaries (e.g., "**/test/**", "dir/**/bar.yaml").
// Patterns without "/" match against any single path segment (like .gitignore).
func matchGlob(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	// Patterns without "/" match against any path segment.
	if !strings.Contains(pattern, "/") {
		for _, seg := range strings.Split(path, "/") {
			if matched, err := filepath.Match(pattern, seg); err == nil && matched {
				return true
			}
		}
		return false
	}

	return matchSegments(
		strings.Split(pattern, "/"),
		strings.Split(path, "/"),
	)
}

func matchSegments(pattern, path []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := range len(path) + 1 {
				if matchSegments(pattern, path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		matched, err := filepath.Match(pattern[0], path[0])
		if err != nil || !matched {
			return false
		}
		pattern = pattern[1:]
		path = path[1:]
	}
	return len(path) == 0
}
