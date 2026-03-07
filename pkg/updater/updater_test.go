package updater

import (
	"strings"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/manifest"
	"gopkg.in/yaml.v3"
)

func TestUpdateBytes_SingleSource(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cert-manager
spec:
  source:
    repoURL: https://charts.jetstack.io
    chart: cert-manager
    targetRevision: 1.13.2 # pinned version
`

	ref := manifest.ChartReference{
		ChartName:      "cert-manager",
		TargetRevision: "1.13.2",
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	result, err := UpdateBytes([]byte(input), &ref, "1.14.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(result)
	if !strings.Contains(output, "1.14.1") {
		t.Error("expected output to contain new version 1.14.1")
	}
	if strings.Contains(output, "1.13.2") {
		t.Error("expected output to not contain old version 1.13.2")
	}
}

func TestUpdateBytes_MultiSource(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: grafana
spec:
  sources:
    - repoURL: https://github.com/myorg/gitops-config.git
      targetRevision: main
      ref: values
    - repoURL: https://grafana.github.io/helm-charts
      chart: grafana
      targetRevision: 7.0.1
`

	ref := manifest.ChartReference{
		ChartName:      "grafana",
		TargetRevision: "7.0.1",
		YAMLPath:       "spec.sources[1].targetRevision",
		SourceIndex:    1,
	}

	result, err := UpdateBytes([]byte(input), &ref, "8.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(result)
	if !strings.Contains(output, "8.2.0") {
		t.Error("expected output to contain new version 8.2.0")
	}
	// The values ref should still point to "main"
	if !strings.Contains(output, "main") {
		t.Error("expected output to still contain 'main' for values ref")
	}
}

func TestUpdateBytes_VersionMismatch(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test
spec:
  source:
    targetRevision: 1.0.0
`

	ref := manifest.ChartReference{
		TargetRevision: "2.0.0", // doesn't match
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	_, err := UpdateBytes([]byte(input), &ref, "3.0.0")
	if err == nil {
		t.Error("expected error for version mismatch")
	}
}

func TestUpdateBytes_Preserves4SpaceIndentAndComments(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
    name: cert-manager  # application name
spec:
    source:
        repoURL: https://charts.jetstack.io
        chart: cert-manager
        targetRevision: 1.13.2 # pinned version
    # destination config
    destination:
        server: https://kubernetes.default.svc
`

	ref := manifest.ChartReference{
		ChartName:      "cert-manager",
		TargetRevision: "1.13.2",
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	result, err := UpdateBytes([]byte(input), &ref, "1.14.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(result)

	// Version should be updated
	if !strings.Contains(output, "1.14.1") {
		t.Error("expected output to contain new version 1.14.1")
	}
	if strings.Contains(output, "1.13.2") {
		t.Error("expected output to not contain old version 1.13.2")
	}

	// 4-space indentation must be preserved
	if !strings.Contains(output, "    name: cert-manager") {
		t.Error("expected 4-space indentation to be preserved")
	}
	if !strings.Contains(output, "        targetRevision: 1.14.1") {
		t.Error("expected 4-space indentation at targetRevision")
	}

	// Comments must be preserved
	if !strings.Contains(output, "# pinned version") {
		t.Error("expected inline comment to be preserved")
	}
	if !strings.Contains(output, "# application name") {
		t.Error("expected inline comment on name to be preserved")
	}
	if !strings.Contains(output, "# destination config") {
		t.Error("expected standalone comment to be preserved")
	}
}

func TestUpdateBytes_QuotedVersion(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    chart: test
    targetRevision: "1.0.0"
`

	ref := manifest.ChartReference{
		ChartName:      "test",
		TargetRevision: "1.0.0",
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	result, err := UpdateBytes([]byte(input), &ref, "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(result)
	if !strings.Contains(output, `"2.0.0"`) {
		t.Errorf("expected quoted version in output, got: %s", output)
	}
}

func TestNavigateToNode_InvalidPath(t *testing.T) {
	input := `spec:
  source:
    targetRevision: 1.0.0
`

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}

	_, err := navigateToNode(doc.Content[0], "spec.nonexistent.field")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestParsePathPart(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantIdx  int
	}{
		{"simple", "simple", -1},
		{"sources[0]", "sources", 0},
		{"sources[abc]", "sources[abc]", -1},
		{"sources[0", "sources[0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, idx := parsePathPart(tt.input)
			if name != tt.wantName {
				t.Errorf("parsePathPart(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
			if idx != tt.wantIdx {
				t.Errorf("parsePathPart(%q) idx = %d, want %d", tt.input, idx, tt.wantIdx)
			}
		})
	}
}

func TestIndexSequence_NonSequenceNode(t *testing.T) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	_, err := indexSequence(node, "items[0]", 0)
	if err == nil {
		t.Error("expected error for non-sequence node")
	}
	if !strings.Contains(err.Error(), "expected sequence") {
		t.Errorf("expected 'expected sequence' in error, got: %v", err)
	}
}

func TestIndexSequence_OutOfBounds(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.SequenceNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "only-item"},
		},
	}
	_, err := indexSequence(node, "items[5]", 5)
	if err == nil {
		t.Error("expected error for out-of-bounds index")
	}
	if !strings.Contains(err.Error(), "out of bounds") {
		t.Errorf("expected 'out of bounds' in error, got: %v", err)
	}
}

func TestUpdateBytes_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`{{{not valid yaml:::`)
	ref := &manifest.ChartReference{
		YAMLPath:       "spec.source.targetRevision",
		TargetRevision: "1.0.0",
		SourceIndex:    -1,
	}
	_, err := UpdateBytes(invalidYAML, ref, "2.0.0")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestUpdateBytes_EmptyDocument(t *testing.T) {
	emptyYAML := []byte("")
	ref := &manifest.ChartReference{
		YAMLPath:       "spec.source.targetRevision",
		TargetRevision: "1.0.0",
		SourceIndex:    -1,
	}
	_, err := UpdateBytes(emptyYAML, ref, "2.0.0")
	if err == nil {
		t.Error("expected error for empty document")
	}
	if !strings.Contains(err.Error(), "no YAML documents") {
		t.Errorf("expected 'no YAML documents' in error, got: %v", err)
	}
}

func TestFindMappingKey_NonMapping(t *testing.T) {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: "hello"}
	_, err := findMappingKey(node, "somekey")
	if err == nil {
		t.Error("expected error for non-mapping node")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestUpdateBytes_SingleQuotedVersion(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    chart: test
    targetRevision: '1.0.0'
`

	ref := manifest.ChartReference{
		ChartName:      "test",
		TargetRevision: "1.0.0",
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	result, err := UpdateBytes([]byte(input), &ref, "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(result)
	if !strings.Contains(output, `'2.0.0'`) {
		t.Errorf("expected single-quoted version in output, got: %s", output)
	}
}

func TestFindTargetNode_EmptyDocContent(t *testing.T) {
	// doc with kind = DocumentNode but no content
	docs := []*yaml.Node{
		{Kind: yaml.DocumentNode, Content: nil},
	}
	ref := &manifest.ChartReference{
		YAMLPath:       "spec.source.targetRevision",
		TargetRevision: "1.0.0",
	}
	_, found := findTargetNode(docs, ref)
	if found {
		t.Error("expected not found for empty doc content")
	}
}

func TestFindTargetNode_NonDocumentNode(t *testing.T) {
	// Node that isn't a DocumentNode
	docs := []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "hello"},
	}
	ref := &manifest.ChartReference{
		YAMLPath:       "spec.source.targetRevision",
		TargetRevision: "1.0.0",
	}
	_, found := findTargetNode(docs, ref)
	if found {
		t.Error("expected not found for non-document node")
	}
}

func TestNavigateToNode_SequenceIndexPath(t *testing.T) {
	input := `sources:
  - targetRevision: 1.0.0
  - targetRevision: 2.0.0
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}

	node, err := navigateToNode(doc.Content[0], "sources[1].targetRevision")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Value != "2.0.0" {
		t.Errorf("expected 2.0.0, got %s", node.Value)
	}
}

func TestUpdateBytes_DoubleQuotedWithEscapes(t *testing.T) {
	// Test quoted value replacement preserving quote style when backslash-escaped chars exist
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    chart: test
    targetRevision: "1.0.0"
    extra: "has \"quotes\" inside"
`

	ref := manifest.ChartReference{
		ChartName:      "test",
		TargetRevision: "1.0.0",
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	result, err := UpdateBytes([]byte(input), &ref, "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(result)
	if !strings.Contains(output, `"2.0.0"`) {
		t.Errorf("expected quoted version in output, got: %s", output)
	}
}

func TestNavigateToNode_SequenceIndexOutOfBounds(t *testing.T) {
	input := `sources:
  - targetRevision: 1.0.0
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}

	_, err := navigateToNode(doc.Content[0], "sources[5].targetRevision")
	if err == nil {
		t.Error("expected error for out-of-bounds sequence index in path")
	}
}

func TestUpdateBytes_MultiDocFirstDocNavFails(t *testing.T) {
	// First doc doesn't have spec.source.targetRevision, second doc does
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config
data:
  key: value
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app
spec:
  source:
    targetRevision: 1.0.0
`
	ref := &manifest.ChartReference{
		TargetRevision: "1.0.0",
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}

	result, err := UpdateBytes([]byte(input), ref, "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := string(result)
	if !strings.Contains(output, "2.0.0") {
		t.Error("expected output to contain new version 2.0.0")
	}
	if strings.Contains(output, "targetRevision: 1.0.0") {
		t.Error("expected old version to be replaced")
	}
}

func TestUpdateBytes_MultiDocVersionMismatch(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app1
spec:
  source:
    targetRevision: 1.0.0
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app2
spec:
  source:
    targetRevision: 2.0.0
`
	ref := &manifest.ChartReference{
		TargetRevision: "9.9.9", // matches nothing
		YAMLPath:       "spec.source.targetRevision",
		SourceIndex:    -1,
	}
	_, err := UpdateBytes([]byte(input), ref, "3.0.0")
	if err == nil {
		t.Error("expected error when target version not found in any document")
	}
	if !strings.Contains(err.Error(), "could not find") {
		t.Errorf("expected 'could not find' in error, got: %v", err)
	}
}
