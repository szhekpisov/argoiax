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
