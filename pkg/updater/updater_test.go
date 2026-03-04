package updater

import (
	"strings"
	"testing"

	"github.com/vertrost/argoiax/pkg/manifest"
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

	result, err := UpdateBytes([]byte(input), ref, "1.14.1")
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

	result, err := UpdateBytes([]byte(input), ref, "8.2.0")
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

	_, err := UpdateBytes([]byte(input), ref, "3.0.0")
	if err == nil {
		t.Error("expected error for version mismatch")
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
