package updater

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/vertrost/argoiax/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// UpdateBytes updates the targetRevision in raw YAML bytes, preserving formatting.
// Supports multi-document YAML files.
func UpdateBytes(data []byte, ref *manifest.ChartReference, newVersion string) ([]byte, error) {
	docs, err := parseDocuments(data)
	if err != nil {
		return nil, err
	}

	if !findAndUpdateNode(docs, ref, newVersion) {
		return nil, fmt.Errorf("could not find %s with value %q", ref.YAMLPath, ref.TargetRevision)
	}

	return encodeDocuments(docs)
}

func parseDocuments(data []byte) ([]*yaml.Node, error) {
	var docs []*yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		docs = append(docs, &doc)
	}
	if len(docs) == 0 {
		return nil, errors.New("no YAML documents found")
	}
	return docs, nil
}

func findAndUpdateNode(docs []*yaml.Node, ref *manifest.ChartReference, newVersion string) bool {
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
		return true
	}
	return false
}

func encodeDocuments(docs []*yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("encoding YAML: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("closing YAML encoder: %w", err)
	}
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
			child, err := findMappingKey(current, name)
			if err != nil {
				return nil, err
			}
			current = child
		}

		if idx >= 0 {
			child, err := indexSequence(current, part, idx)
			if err != nil {
				return nil, err
			}
			current = child
		}
	}

	return current, nil
}

// findMappingKey looks up a key in a YAML mapping node and returns its value node.
func findMappingKey(node *yaml.Node, key string) (*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("key %q not found", key)
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], nil
		}
	}
	return nil, fmt.Errorf("key %q not found", key)
}

// indexSequence returns the element at the given index in a YAML sequence node.
func indexSequence(node *yaml.Node, part string, idx int) (*yaml.Node, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("expected sequence at %q, got %v", part, node.Kind)
	}
	if idx >= len(node.Content) {
		return nil, fmt.Errorf("index %d out of bounds (len=%d)", idx, len(node.Content))
	}
	return node.Content[idx], nil
}

// parsePathPart parses "sources[0]" into ("sources", 0) or "source" into ("source", -1).
func parsePathPart(part string) (name string, idx int) {
	bracketIdx := strings.Index(part, "[")
	if bracketIdx == -1 {
		return part, -1
	}
	if !strings.HasSuffix(part, "]") {
		return part, -1
	}
	name = part[:bracketIdx]
	idxStr := part[bracketIdx+1 : len(part)-1]
	var err error
	idx, err = strconv.Atoi(idxStr)
	if err != nil {
		return part, -1
	}
	return name, idx
}
