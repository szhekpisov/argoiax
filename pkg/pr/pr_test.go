package pr

import (
	"strings"
	"testing"
)

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean name unchanged",
			input:    "argoiax/cert-manager-1.14.1",
			expected: "argoiax/cert-manager-1.14.1",
		},
		{
			name:     "spaces replaced",
			input:    "argoiax/my chart-1.0.0",
			expected: "argoiax/my-chart-1.0.0",
		},
		{
			name:     "special chars replaced",
			input:    "argoiax/chart~name^ver:1?2*3[test]",
			expected: "argoiax/chart-name-ver-1-2-3-test",
		},
		{
			name:     "backslash replaced",
			input:    `argoiax\chart`,
			expected: "argoiax-chart",
		},
		{
			name:     "double dots replaced",
			input:    "argoiax/chart..version",
			expected: "argoiax/chart-version",
		},
		{
			name:     "at-brace replaced",
			input:    "argoiax/chart@{version}",
			expected: "argoiax/chart-version",
		},
		{
			name:     "consecutive dashes collapsed",
			input:    "argoiax/chart---name",
			expected: "argoiax/chart-name",
		},
		{
			name:     "leading dot and dash removed",
			input:    ".-argoiax/chart",
			expected: "argoiax/chart",
		},
		{
			name:     "trailing dot and dash removed",
			input:    "argoiax/chart-.",
			expected: "argoiax/chart",
		},
		{
			name:     "long name truncated",
			input:    "argoiax/" + strings.Repeat("a", 250),
			expected: "argoiax/" + strings.Repeat("a", 192),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeBranchName(tc.input)
			if got != tc.expected {
				t.Errorf("SanitizeBranchName(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestRenderTemplate(t *testing.T) {
	info := UpdateInfo{
		ChartName:  "cert-manager",
		NewVersion: "1.14.1",
	}
	result, err := RenderTemplate("argoiax/{{.ChartName}}-{{.NewVersion}}", info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "argoiax/cert-manager-1.14.1" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestRenderTemplate_InvalidTemplate(t *testing.T) {
	_, err := RenderTemplate("{{.Invalid", nil)
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

func TestRenderTemplate_ExecuteError(t *testing.T) {
	// Use a template that calls a method on a nil value, causing an execute-time error.
	_, err := RenderTemplate("{{.NonExistent.Sub}}", (*UpdateInfo)(nil))
	if err == nil {
		t.Error("expected error for template execution failure")
	}
	if !strings.Contains(err.Error(), "executing template") {
		t.Errorf("expected 'executing template' in error, got: %v", err)
	}
}

func TestNewGroupTemplateData_SingleFile(t *testing.T) {
	group := UpdateGroup{
		Updates: []UpdateInfo{
			{ChartName: "cert-manager", OldVersion: "1.0.0", NewVersion: "1.1.0", FilePath: "apps/infra.yaml"},
			{ChartName: "nginx", OldVersion: "4.0.0", NewVersion: "4.1.0", FilePath: "apps/infra.yaml"},
		},
		Files: []FileUpdate{
			{FilePath: "apps/infra.yaml"},
		},
	}

	data := NewGroupTemplateData(group)

	if data.Count != 2 {
		t.Errorf("Count = %d, want 2", data.Count)
	}
	if data.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", data.FileCount)
	}
	if data.FilePath != "apps/infra.yaml" {
		t.Errorf("FilePath = %q, want %q", data.FilePath, "apps/infra.yaml")
	}
	if data.FileBaseName != "infra" {
		t.Errorf("FileBaseName = %q, want %q", data.FileBaseName, "infra")
	}
	if data.Summary != "cert-manager, nginx" {
		t.Errorf("Summary = %q, want %q", data.Summary, "cert-manager, nginx")
	}
	if len(data.ChartNames) != 2 {
		t.Errorf("ChartNames len = %d, want 2", len(data.ChartNames))
	}
}

func TestNewGroupTemplateData_MultipleFiles(t *testing.T) {
	group := UpdateGroup{
		Updates: []UpdateInfo{
			{ChartName: "chart-a"},
			{ChartName: "chart-b"},
		},
		Files: []FileUpdate{
			{FilePath: "a.yaml"},
			{FilePath: "b.yaml"},
		},
	}

	data := NewGroupTemplateData(group)

	if data.FileBaseName != "batch" {
		t.Errorf("FileBaseName = %q, want %q", data.FileBaseName, "batch")
	}
	if data.FilePath != "" {
		t.Errorf("FilePath = %q, want empty for multi-file", data.FilePath)
	}
}

func TestNewGroupTemplateData_DeduplicatesChartNames(t *testing.T) {
	group := UpdateGroup{
		Updates: []UpdateInfo{
			{ChartName: "same-chart", FilePath: "a.yaml"},
			{ChartName: "same-chart", FilePath: "b.yaml"},
		},
		Files: []FileUpdate{
			{FilePath: "a.yaml"},
			{FilePath: "b.yaml"},
		},
	}

	data := NewGroupTemplateData(group)

	if len(data.ChartNames) != 1 {
		t.Errorf("expected 1 unique chart name, got %d", len(data.ChartNames))
	}
	if data.Summary != "same-chart" {
		t.Errorf("Summary = %q, want %q", data.Summary, "same-chart")
	}
}
