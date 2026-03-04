package manifest

import (
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
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding YAML in %s: %w", filePath, err)
		}

		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			continue
		}

		root := doc.Content[0]
		if root.Kind != yaml.MappingNode {
			continue
		}

		kind := getMapValue(root, "kind")
		if kind == "" {
			continue
		}

		apiVersion := getMapValue(root, "apiVersion")
		if !strings.HasPrefix(apiVersion, "argoproj.io/") {
			continue
		}

		switch kind {
		case "Application":
			specNode := getMapNode(root, "spec")
			if specNode == nil {
				continue
			}
			extracted := extractFromSpec(specNode, filePath, "spec", false)
			refs = append(refs, extracted...)

		case "ApplicationSet":
			specNode := getMapNode(root, "spec")
			if specNode == nil {
				continue
			}
			templateNode := getMapNode(specNode, "template")
			if templateNode == nil {
				continue
			}
			templateSpecNode := getMapNode(templateNode, "spec")
			if templateSpecNode == nil {
				continue
			}
			extracted := extractFromSpec(templateSpecNode, filePath, "spec.template.spec", true)
			refs = append(refs, extracted...)
		}
	}

	return refs, nil
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
	ref := getMapValue(sourceNode, "ref")

	// Skip ref-only sources (values references)
	if ref != "" && chart == "" {
		slog.Debug("skipping ref-only source", "file", filePath, "ref", ref)
		return ChartReference{}, false
	}

	// Skip if no targetRevision
	if targetRevision == "" {
		return ChartReference{}, false
	}

	// Skip Go template expressions in ApplicationSets
	if isAppSet && isTemplateExpression(targetRevision) {
		slog.Debug("skipping template expression", "file", filePath, "targetRevision", targetRevision)
		return ChartReference{}, false
	}

	// Skip non-semver targetRevision (HEAD, branch names, etc.)
	if !looksLikeSemver(targetRevision) {
		slog.Debug("skipping non-semver targetRevision", "file", filePath, "targetRevision", targetRevision)
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

	switch {
	case strings.HasPrefix(repoURL, "oci://"):
		cr.Type = SourceTypeOCI
		// For OCI, chart name is the last path segment of the URL
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

	return cr, true
}

func isTemplateExpression(s string) bool {
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}

func looksLikeSemver(s string) bool {
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return false
	}
	// Must start with a digit and contain at least one dot
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	return strings.Contains(s, ".")
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
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, filePath)
}
