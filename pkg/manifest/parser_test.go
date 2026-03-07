package manifest

import (
	"strings"
	"testing"
)

func TestParse_SingleSourceHTTP(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cert-manager
spec:
  source:
    repoURL: https://charts.jetstack.io
    chart: cert-manager
    targetRevision: 1.13.2
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	ref := refs[0]
	if ref.ChartName != "cert-manager" {
		t.Errorf("expected chart name cert-manager, got %s", ref.ChartName)
	}
	if ref.RepoURL != "https://charts.jetstack.io" {
		t.Errorf("expected repo URL https://charts.jetstack.io, got %s", ref.RepoURL)
	}
	if ref.TargetRevision != "1.13.2" {
		t.Errorf("expected target revision 1.13.2, got %s", ref.TargetRevision)
	}
	if ref.Type != SourceTypeHTTP {
		t.Errorf("expected HTTP source type, got %s", ref.Type)
	}
	if ref.YAMLPath != "spec.source.targetRevision" {
		t.Errorf("expected YAML path spec.source.targetRevision, got %s", ref.YAMLPath)
	}
	if ref.SourceIndex != -1 {
		t.Errorf("expected source index -1, got %d", ref.SourceIndex)
	}
}

func TestParse_SingleSourceOCI(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: karpenter
spec:
  source:
    repoURL: oci://public.ecr.aws/karpenter/karpenter
    targetRevision: 0.33.0
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	ref := refs[0]
	if ref.Type != SourceTypeOCI {
		t.Errorf("expected OCI source type, got %s", ref.Type)
	}
	if ref.ChartName != "karpenter" {
		t.Errorf("expected chart name karpenter, got %s", ref.ChartName)
	}
	if ref.TargetRevision != "0.33.0" {
		t.Errorf("expected target revision 0.33.0, got %s", ref.TargetRevision)
	}
}

func TestParse_SingleSourceGit(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://github.com/myorg/helm-charts.git
    path: charts/myapp
    targetRevision: v2.1.0
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	ref := refs[0]
	if ref.Type != SourceTypeGit {
		t.Errorf("expected Git source type, got %s", ref.Type)
	}
	if ref.ChartName != "myapp" {
		t.Errorf("expected chart name myapp, got %s", ref.ChartName)
	}
}

func TestParse_MultiSource(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
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
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ref-only source (with ref: values and non-semver targetRevision "main") should be skipped
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref (ref-only skipped), got %d", len(refs))
	}

	ref := refs[0]
	if ref.ChartName != "grafana" {
		t.Errorf("expected chart name grafana, got %s", ref.ChartName)
	}
	if ref.SourceIndex != 1 {
		t.Errorf("expected source index 1, got %d", ref.SourceIndex)
	}
	if ref.YAMLPath != "spec.sources[1].targetRevision" {
		t.Errorf("expected YAML path spec.sources[1].targetRevision, got %s", ref.YAMLPath)
	}
}

func TestParse_ApplicationSet(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: ingress-nginx
spec:
  generators:
    - clusters: {}
  template:
    spec:
      source:
        repoURL: https://kubernetes.github.io/ingress-nginx
        chart: ingress-nginx
        targetRevision: 4.9.0
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	ref := refs[0]
	if ref.ChartName != "ingress-nginx" {
		t.Errorf("expected chart name ingress-nginx, got %s", ref.ChartName)
	}
	if !ref.IsApplicationSet {
		t.Error("expected IsApplicationSet to be true")
	}
	if ref.YAMLPath != "spec.template.spec.source.targetRevision" {
		t.Errorf("expected YAML path spec.template.spec.source.targetRevision, got %s", ref.YAMLPath)
	}
}

func TestParse_ApplicationSetSkipsTemplateExpressions(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: dynamic
spec:
  template:
    spec:
      source:
        repoURL: '{{repoURL}}'
        chart: '{{chart}}'
        targetRevision: '{{version}}'
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs (template expressions skipped), got %d", len(refs))
	}
}

func TestParse_MultiDocument(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: prometheus
spec:
  source:
    repoURL: https://prometheus-community.github.io/helm-charts
    chart: kube-prometheus-stack
    targetRevision: 55.5.0
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: loki
spec:
  source:
    repoURL: https://grafana.github.io/helm-charts
    chart: loki
    targetRevision: 5.41.0
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	if refs[0].ChartName != "kube-prometheus-stack" {
		t.Errorf("expected first chart kube-prometheus-stack, got %s", refs[0].ChartName)
	}
	if refs[1].ChartName != "loki" {
		t.Errorf("expected second chart loki, got %s", refs[1].ChartName)
	}
}

func TestParse_NonArgoCDYAML(t *testing.T) {
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 1
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs for non-ArgoCD YAML, got %d", len(refs))
	}
}

func TestParse_SkipsNonSemver(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://github.com/myorg/repo.git
    path: charts/myapp
    targetRevision: HEAD
`
	refs, err := Parse(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs (HEAD is not semver), got %d", len(refs))
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	input := `{{{not valid yaml:::`
	_, err := Parse(strings.NewReader(input), "invalid.yaml")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParse_EmptyDocument(t *testing.T) {
	input := `# just a comment
`
	refs, err := Parse(strings.NewReader(input), "empty.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for empty document, got %d", len(refs))
	}
}

func TestParse_AppSetMissingTemplate(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: no-template
spec:
  generators:
    - clusters: {}
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for AppSet without template, got %d", len(refs))
	}
}

func TestParse_AppSetMissingTemplateSpec(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: no-template-spec
spec:
  generators:
    - clusters: {}
  template:
    metadata:
      name: test
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for AppSet without template.spec, got %d", len(refs))
	}
}

func TestParse_NonMappingRoot(t *testing.T) {
	input := `
- item1
- item2
- item3
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for non-mapping root, got %d", len(refs))
	}
}

func TestParse_ScalarSpec(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec: not-a-mapping
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for scalar spec, got %d", len(refs))
	}
}

func TestParse_AppSetScalarTemplate(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: test
spec:
  generators:
    - clusters: {}
  template: not-a-mapping
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for scalar template, got %d", len(refs))
	}
}

func TestParse_NonMappingSourceInSequence(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  sources:
    - not-a-mapping
    - repoURL: https://charts.example.com
      chart: mychart
      targetRevision: 1.0.0
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The non-mapping entry should be skipped, only the valid one returned
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref (non-mapping skipped), got %d", len(refs))
	}
	if refs[0].ChartName != "mychart" {
		t.Errorf("expected mychart, got %s", refs[0].ChartName)
	}
}

func TestParse_AppSetMissingSpec(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: no-spec
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for AppSet without spec, got %d", len(refs))
	}
}

func TestParse_ApplicationMissingSpec(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: no-spec
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for Application without spec, got %d", len(refs))
	}
}

func TestParse_UnknownKind(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: default
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for unknown kind, got %d", len(refs))
	}
}

func TestParse_SourceWithRefOnly(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  sources:
    - repoURL: https://github.com/myorg/config.git
      targetRevision: 1.0.0
      ref: values
    - repoURL: https://charts.example.com
      chart: mychart
      targetRevision: 2.0.0
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First source has ref and no chart, so it should be skipped
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref (ref-only skipped), got %d", len(refs))
	}
	if refs[0].ChartName != "mychart" {
		t.Errorf("expected mychart, got %s", refs[0].ChartName)
	}
}

func TestParse_EmptyTargetRevision(t *testing.T) {
	input := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp
spec:
  source:
    repoURL: https://charts.example.com
    chart: mychart
    targetRevision: ""
`
	refs, err := Parse(strings.NewReader(input), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for empty targetRevision, got %d", len(refs))
	}
}

func TestLooksLikeSemver(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1.2.3", true},
		{"v1.2.3", true},
		{"0.33.0", true},
		{"55.5.0", true},
		{"HEAD", false},
		{"main", false},
		{"develop", false},
		{"", false},
		{"v1", false},
		{"1.2.3-rc1", true},
		{"1.abc", false},
		{"3.", false},
		{"99.z", false},
		{"abc.1", false},
	}

	for _, tt := range tests {
		if got := looksLikeSemver(tt.input); got != tt.want {
			t.Errorf("looksLikeSemver(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
