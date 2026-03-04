package updater

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/vertrost/argoiax/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// UpdateBytes updates the targetRevision in raw YAML bytes, preserving formatting.
// Supports multi-document YAML files.
func UpdateBytes(data []byte, ref manifest.ChartReference, newVersion string) ([]byte, error) {
	// Parse all documents
	var docs []*yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		docs = append(docs, &doc)
	}

	if len(docs) == 0 {
		return nil, fmt.Errorf("no YAML documents found")
	}

	// Try each document to find the target node
	found := false
	for _, doc := range docs {
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			continue
		}
		node, err := navigateToNode(doc.Content[0], ref.YAMLPath)
		if err != nil {
			continue
		}
		if node.Value != ref.TargetRevision {
			continue
		}
		node.Value = newVersion
		found = true
		break
	}

	if !found {
		return nil, fmt.Errorf("could not find %s with value %q", ref.YAMLPath, ref.TargetRevision)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("encoding YAML: %w", err)
		}
	}
	enc.Close()

	return buf.Bytes(), nil
}

// navigateToNode follows a dot-separated YAML path to find the target node.
// Supports array notation like "sources[0]".
func navigateToNode(node *yaml.Node, path string) (*yaml.Node, error) {
	parts := strings.Split(path, ".")
	current := node

	for _, part := range parts {
		name, idx := parsePathPart(part)

		if name != "" {
			found := false
			if current.Kind == yaml.MappingNode {
				for i := 0; i < len(current.Content)-1; i += 2 {
					if current.Content[i].Value == name {
						current = current.Content[i+1]
						found = true
						break
					}
				}
			}
			if !found {
				return nil, fmt.Errorf("key %q not found", name)
			}
		}

		if idx >= 0 {
			if current.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("expected sequence at %q, got %v", part, current.Kind)
			}
			if idx >= len(current.Content) {
				return nil, fmt.Errorf("index %d out of bounds (len=%d)", idx, len(current.Content))
			}
			current = current.Content[idx]
		}
	}

	return current, nil
}

// parsePathPart parses "sources[0]" into ("sources", 0) or "source" into ("source", -1).
func parsePathPart(part string) (string, int) {
	bracketIdx := strings.Index(part, "[")
	if bracketIdx == -1 {
		return part, -1
	}
	if !strings.HasSuffix(part, "]") {
		return part, -1
	}
	name := part[:bracketIdx]
	idxStr := part[bracketIdx+1 : len(part)-1]
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return part, -1
	}
	return name, idx
}
