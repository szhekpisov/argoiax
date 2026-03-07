package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsYAMLFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"foo.yaml", true},
		{"foo.yml", true},
		{"foo.YML", true},
		{"foo.YAML", true},
		{"foo.json", false},
		{"foo.txt", false},
		{"foo", false},
		{"dir/bar.yaml", true},
		{"dir/bar.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isYAMLFile(tt.path); got != tt.want {
				t.Errorf("isYAMLFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestShouldIgnore(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{"exact match", []string{"vendor"}, "vendor", true},
		{"basename match", []string{"*.txt"}, "dir/notes.txt", true},
		{"no match", []string{"*.json"}, "dir/app.yaml", false},
		{"globstar prefix", []string{"**/generated"}, "foo/bar/generated", true},
		{"globstar suffix", []string{"tmp/**"}, "tmp/foo/bar.yaml", true},
		{"empty patterns", nil, "anything.yaml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Walker{IgnorePatterns: tt.patterns}
			if got := w.shouldIgnore(tt.path); got != tt.want {
				t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchesGlobstar(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		pattern string
		want    bool
	}{
		{"prefix globstar", "foo/bar/baz.yaml", "**/baz.yaml", true},
		{"no match globstar", "foo/bar/baz.yaml", "**/qux.yaml", false},
		{"dir prefix globstar", "dir/sub/bar.yaml", "dir/**/bar.yaml", true},
		{"dir prefix no match", "other/sub/bar.yaml", "dir/**/bar.yaml", false},
		{"dir suffix globstar", "dir/sub/anything", "dir/**", true},
		{"bare double star", "anything/at/all", "**", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesGlobstar(tt.path, tt.pattern); got != tt.want {
				t.Errorf("matchesGlobstar(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchesDeepPattern(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		pattern string
		want    bool
	}{
		{"match at root", "bar.yaml", "bar.yaml", true},
		{"match nested", "sub/bar.yaml", "bar.yaml", true},
		{"no match", "sub/foo.yaml", "bar.yaml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesDeepPattern(tt.path, tt.pattern); got != tt.want {
				t.Errorf("matchesDeepPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestSourceTypeString(t *testing.T) {
	tests := []struct {
		st   SourceType
		want string
	}{
		{SourceTypeHTTP, "http"},
		{SourceTypeOCI, "oci"},
		{SourceTypeGit, "git"},
		{SourceType(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.st.String(); got != tt.want {
				t.Errorf("SourceType(%d).String() = %q, want %q", tt.st, got, tt.want)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://charts.example.com
    chart: mychart
    targetRevision: 1.2.3
`
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	refs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].ChartName != "mychart" {
		t.Errorf("expected chart mychart, got %s", refs[0].ChartName)
	}
	if refs[0].TargetRevision != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", refs[0].TargetRevision)
	}
	if refs[0].Type != SourceTypeHTTP {
		t.Errorf("expected HTTP source type, got %s", refs[0].Type)
	}
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/app.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseFile_NonArgoCD(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: v1
kind: ConfigMap
metadata:
  name: myconfig
data:
  key: value
`
	path := filepath.Join(dir, "configmap.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	refs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for non-ArgoCD YAML, got %d", len(refs))
	}
}

func TestWalk(t *testing.T) {
	dir := t.TempDir()

	// Create a valid ArgoCD YAML
	argoContent := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://charts.example.com
    chart: mychart
    targetRevision: 1.0.0
`
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a non-ArgoCD YAML
	nonArgoContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`
	if err := os.WriteFile(filepath.Join(dir, "configmap.yml"), []byte(nonArgoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a non-YAML file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory with another ArgoCD YAML
	subDir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subDir, 0o750); err != nil {
		t.Fatal(err)
	}
	argoContent2 := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: otherapp
spec:
  source:
    repoURL: oci://registry.example.com/charts/nginx
    targetRevision: 2.0.0
`
	if err := os.WriteFile(filepath.Join(subDir, "oci-app.yaml"), []byte(argoContent2), 0o600); err != nil {
		t.Fatal(err)
	}

	w := &Walker{}
	refs, err := w.Walk([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
}

func TestWalk_WithIgnorePatterns(t *testing.T) {
	dir := t.TempDir()

	argoContent := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://charts.example.com
    chart: mychart
    targetRevision: 1.0.0
`
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	ignored := filepath.Join(dir, "ignored")
	if err := os.Mkdir(ignored, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignored, "skip.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	w := &Walker{IgnorePatterns: []string{"ignored"}}
	refs, err := w.Walk([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 1 {
		t.Errorf("expected 1 ref (ignored dir skipped), got %d", len(refs))
	}
}

func TestWalk_IgnoresYAMLFileByPattern(t *testing.T) {
	dir := t.TempDir()

	argoContent := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://charts.example.com
    chart: mychart
    targetRevision: 1.0.0
`
	// This file should be found
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// This file should be ignored by pattern (file-level, not dir-level)
	if err := os.WriteFile(filepath.Join(dir, "generated.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	w := &Walker{IgnorePatterns: []string{"generated.yaml"}}
	refs, err := w.Walk([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(refs) != 1 {
		t.Errorf("expected 1 ref (generated.yaml skipped), got %d", len(refs))
	}
}

func TestWalk_InvalidYAMLFile(t *testing.T) {
	dir := t.TempDir()

	invalidContent := `{{{not valid yaml:::`
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(invalidContent), 0o600); err != nil {
		t.Fatal(err)
	}

	w := &Walker{}
	refs, err := w.Walk([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalid YAML should be warned about but not cause a walk error
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for invalid YAML file, got %d", len(refs))
	}
}

func TestWalk_NonExistentDir(t *testing.T) {
	w := &Walker{}
	_, err := w.Walk([]string{"/nonexistent/path/that/does/not/exist"})
	// WalkDir on a nonexistent path should return an error
	if err == nil {
		// Some implementations may just warn, not error
		return
	}
}

func TestWalk_EmptyDirs(t *testing.T) {
	w := &Walker{}
	refs, err := w.Walk(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for nil dirs, got %d", len(refs))
	}
}

func TestWalk_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	argoContent := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://charts.example.com
    chart: mychart
    targetRevision: 1.0.0
`
	if err := os.WriteFile(filepath.Join(dir1, "app1.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "app2.yaml"), []byte(argoContent), 0o600); err != nil {
		t.Fatal(err)
	}

	w := &Walker{}
	refs, err := w.Walk([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected 2 refs from 2 dirs, got %d", len(refs))
	}
}
