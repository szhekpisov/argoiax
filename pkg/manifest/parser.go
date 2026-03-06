package manifest

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse reads YAML from r and extracts all ChartReferences from ArgoCD Application/ApplicationSet docs.
func Parse(r io.Reader, filePath string) ([]ChartReference, error) {
	var refs []ChartReference
	decoder := yaml.NewDecoder(r)

	for {
		var doc yaml.Node
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding YAML in %s: %w", filePath, err)
		}

		refs = append(refs, extractFromDoc(&doc, filePath)...)
	}

	return refs, nil
}

func extractFromDoc(doc *yaml.Node, filePath string) []ChartReference {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	kind := getMapValue(root, "kind")
	apiVersion := getMapValue(root, "apiVersion")
	if kind == "" || !strings.HasPrefix(apiVersion, "argoproj.io/") {
		return nil
	}

	switch kind {
	case "Application":
		specNode := getMapNode(root, "spec")
		if specNode == nil {
			return nil
		}
		return extractFromSpec(specNode, filePath, "spec", false)

	case "ApplicationSet":
		return extractFromAppSet(root, filePath)

	default:
		return nil
	}
}

func extractFromAppSet(root *yaml.Node, filePath string) []ChartReference {
	specNode := getMapNode(root, "spec")
	if specNode == nil {
		return nil
	}
	templateNode := getMapNode(specNode, "template")
	if templateNode == nil {
		return nil
	}
	templateSpecNode := getMapNode(templateNode, "spec")
	if templateSpecNode == nil {
		return nil
	}
	return extractFromSpec(templateSpecNode, filePath, "spec.template.spec", true)
}

func extractFromSpec(specNode *yaml.Node, filePath, pathPrefix string, isAppSet bool) []ChartReference {
	var refs []ChartReference

	// Check single source
	sourceNode := getMapNode(specNode, "source")
	if sourceNode != nil {
		if ref, ok := extractFromSource(sourceNode, filePath, pathPrefix+".source", -1, isAppSet); ok {
			refs = append(refs, ref)
		}
	}

	// Check multi-source
	sourcesNode := getMapNode(specNode, "sources")
	if sourcesNode != nil && sourcesNode.Kind == yaml.SequenceNode {
		for i, item := range sourcesNode.Content {
			yamlPath := fmt.Sprintf("%s.sources[%d]", pathPrefix, i)
			if ref, ok := extractFromSource(item, filePath, yamlPath, i, isAppSet); ok {
				refs = append(refs, ref)
			}
		}
	}

	return refs
}

func extractFromSource(sourceNode *yaml.Node, filePath, yamlPath string, sourceIndex int, isAppSet bool) (ChartReference, bool) {
	if sourceNode.Kind != yaml.MappingNode {
		return ChartReference{}, false
	}

	repoURL := getMapValue(sourceNode, "repoURL")
	targetRevision := getMapValue(sourceNode, "targetRevision")
	chart := getMapValue(sourceNode, "chart")

	if !shouldProcess(sourceNode, filePath, targetRevision, chart, isAppSet) {
		return ChartReference{}, false
	}

	cr := ChartReference{
		RepoURL:          repoURL,
		TargetRevision:   targetRevision,
		FilePath:         filePath,
		YAMLPath:         yamlPath + ".targetRevision",
		SourceIndex:      sourceIndex,
		IsApplicationSet: isAppSet,
	}
	classifySource(&cr, sourceNode, repoURL, chart)

	return cr, true
}

func shouldProcess(sourceNode *yaml.Node, filePath, targetRevision, chart string, isAppSet bool) bool {
	ref := getMapValue(sourceNode, "ref")
	if ref != "" && chart == "" {
		slog.Debug("skipping ref-only source", "file", filePath, "ref", ref)
		return false
	}
	if targetRevision == "" {
		return false
	}
	if isAppSet && isTemplateExpression(targetRevision) {
		slog.Debug("skipping template expression", "file", filePath, "targetRevision", targetRevision)
		return false
	}
	if !looksLikeSemver(targetRevision) {
		slog.Debug("skipping non-semver targetRevision", "file", filePath, "targetRevision", targetRevision)
		return false
	}
	return true
}

func classifySource(cr *ChartReference, sourceNode *yaml.Node, repoURL, chart string) {
	switch {
	case strings.HasPrefix(repoURL, "oci://"):
		cr.Type = SourceTypeOCI
		parts := strings.Split(strings.TrimPrefix(repoURL, "oci://"), "/")
		if len(parts) > 0 {
			cr.ChartName = parts[len(parts)-1]
		}
	case chart != "":
		cr.Type = SourceTypeHTTP
		cr.ChartName = chart
	default:
		cr.Type = SourceTypeGit
		path := getMapValue(sourceNode, "path")
		if path != "" {
			parts := strings.Split(path, "/")
			cr.ChartName = parts[len(parts)-1]
		}
	}
}

func isTemplateExpression(s string) bool {
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}

func looksLikeSemver(s string) bool {
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return false
	}
	// Require at least two dot-separated numeric segments (e.g. "1.2")
	dotIdx := strings.IndexByte(s, '.')
	if dotIdx < 1 {
		return false
	}
	for i := range dotIdx {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	// At least one digit must follow the dot
	if dotIdx+1 >= len(s) || s[dotIdx+1] < '0' || s[dotIdx+1] > '9' {
		return false
	}
	return true
}

func getMapValue(node *yaml.Node, key string) string {
	n := getMapNode(node, key)
	if n == nil {
		return ""
	}
	return n.Value
}

func getMapNode(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// ParseFile reads and parses a file at the given path.
func ParseFile(filePath string) ([]ChartReference, error) {
	f, err := os.Open(filePath) //nolint:gosec // path comes from scanRefs file discovery
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Parse(f, filePath)
}
