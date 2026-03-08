package pr

import (
	"strings"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/releasenotes"
)

func TestRenderPRBody_Basic(t *testing.T) {
	info := UpdateInfo{
		ChartName:  "cert-manager",
		RepoURL:    "https://charts.jetstack.io",
		OldVersion: "1.13.2",
		NewVersion: "1.14.1",
		IsBreaking: false,
	}

	body := RenderPRBody(&info)

	if !strings.Contains(body, "Bumps [cert-manager](https://charts.jetstack.io) from 1.13.2 to 1.14.1.") {
		t.Error("expected Dependabot-style opening line")
	}
	if strings.Contains(body, "WARNING") {
		t.Error("did not expect breaking change warning")
	}
	if !strings.Contains(body, "Close this PR") {
		t.Error("expected argoiax commands in footer")
	}
	if !strings.Contains(body, "---") {
		t.Error("expected separator before footer")
	}
}

func TestRenderPRBody_Breaking(t *testing.T) {
	info := UpdateInfo{
		ChartName:       "grafana",
		RepoURL:         "https://grafana.github.io/helm-charts",
		OldVersion:      "7.0.1",
		NewVersion:      "8.2.0",
		IsBreaking:      true,
		BreakingReasons: []string{"Major version bump detected"},
		ReleaseNotes: &releasenotes.Notes{
			SourceURL: "https://github.com/grafana/helm-charts/releases",
		},
	}

	body := RenderPRBody(&info)

	if !strings.Contains(body, "> [!WARNING]") {
		t.Error("expected breaking change warning")
	}
	if !strings.Contains(body, "major version update") {
		t.Error("expected major version update text")
	}
	if !strings.Contains(body, "Major version bump detected") {
		t.Error("expected breaking reason in body")
	}
	if !strings.Contains(body, "breaking-red") {
		t.Error("expected breaking badge")
	}
}

func TestRenderPRBody_WithReleaseNotes(t *testing.T) {
	info := UpdateInfo{
		ChartName:  "cert-manager",
		RepoURL:    "https://charts.jetstack.io",
		OldVersion: "1.13.2",
		NewVersion: "1.14.1",
		ReleaseNotes: &releasenotes.Notes{
			Entries: []releasenotes.Entry{
				{Version: "1.14.1", Body: "- Fixed bug\n- Added feature"},
				{Version: "1.14.0", Body: "- Major refactoring"},
			},
			SourceURL: "https://github.com/cert-manager/cert-manager/releases",
		},
	}

	body := RenderPRBody(&info)

	if !strings.Contains(body, "<details>") {
		t.Error("expected collapsible release notes section")
	}
	if !strings.Contains(body, "<summary>Release notes</summary>") {
		t.Error("expected release notes summary")
	}
	if !strings.Contains(body, "1.14.1") {
		t.Error("expected version 1.14.1 in release notes")
	}
	if !strings.Contains(body, "Fixed bug") {
		t.Error("expected release notes content")
	}
	if !strings.Contains(body, "<blockquote>") {
		t.Error("expected blockquote in release notes")
	}
}

func TestRenderPRBody_NoReleaseNotes(t *testing.T) {
	info := UpdateInfo{
		ChartName:  "test-chart",
		RepoURL:    "https://example.com",
		OldVersion: "1.0.0",
		NewVersion: "1.1.0",
	}

	body := RenderPRBody(&info)

	if strings.Contains(body, "<summary>Release notes</summary>") {
		t.Error("did not expect release notes section")
	}
	if !strings.Contains(body, "Bumps [test-chart](https://example.com)") {
		t.Error("expected opening line")
	}
}

func TestRenderPRBody_BreakingNoSourceURL(t *testing.T) {
	info := UpdateInfo{
		ChartName:       "grafana",
		RepoURL:         "https://grafana.github.io/helm-charts",
		OldVersion:      "7.0.1",
		NewVersion:      "8.2.0",
		IsBreaking:      true,
		BreakingReasons: []string{"Major version bump detected"},
		ReleaseNotes: &releasenotes.Notes{
			SourceURL: "", // empty source URL
			Entries:   []releasenotes.Entry{{Version: "8.2.0", Body: "- Some changes"}},
		},
	}

	body := RenderPRBody(&info)

	if !strings.Contains(body, "> [!WARNING]") {
		t.Error("expected breaking change warning")
	}
	// Should render badge without link when SourceURL is empty
	if !strings.Contains(body, "![Breaking change](https://img.shields.io/badge/change-breaking-red)") {
		t.Error("expected unlinked breaking badge")
	}
	// Should NOT contain a linked badge
	if strings.Contains(body, "[![Breaking change]") {
		t.Error("did not expect linked breaking badge when SourceURL is empty")
	}
}

func TestRenderPRBody_BreakingNoReleaseNotes(t *testing.T) {
	info := UpdateInfo{
		ChartName:       "grafana",
		RepoURL:         "https://grafana.github.io/helm-charts",
		OldVersion:      "7.0.1",
		NewVersion:      "8.2.0",
		IsBreaking:      true,
		BreakingReasons: []string{"Major version bump detected"},
		// ReleaseNotes is nil
	}

	body := RenderPRBody(&info)

	if !strings.Contains(body, "> [!WARNING]") {
		t.Error("expected breaking change warning")
	}
	// Should render unlinked badge when ReleaseNotes is nil
	if !strings.Contains(body, "![Breaking change](https://img.shields.io/badge/change-breaking-red)") {
		t.Error("expected unlinked breaking badge")
	}
	if strings.Contains(body, "[![Breaking change]") {
		t.Error("did not expect linked breaking badge when ReleaseNotes is nil")
	}
}

func TestRenderGroupPRBody_MultipleCharts(t *testing.T) {
	group := UpdateGroup{
		Updates: []UpdateInfo{
			{
				ChartName:  "cert-manager",
				OldVersion: "1.13.2",
				NewVersion: "1.14.1",
				FilePath:   "apps/cert-manager.yaml",
				RepoURL:    "https://charts.jetstack.io",
				ReleaseNotes: &releasenotes.Notes{
					Entries:   []releasenotes.Entry{{Version: "1.14.1", Body: "- Fixed bug"}},
					SourceURL: "https://github.com/cert-manager/cert-manager/releases",
				},
			},
			{
				ChartName:  "nginx",
				OldVersion: "4.8.0",
				NewVersion: "4.9.0",
				FilePath:   "apps/cert-manager.yaml",
				RepoURL:    "https://kubernetes.github.io/ingress-nginx",
			},
		},
		Files: []FileUpdate{{FilePath: "apps/cert-manager.yaml"}},
	}

	body := RenderGroupPRBody(group)

	if !strings.Contains(body, "## Updated Charts") {
		t.Error("expected summary table header")
	}
	if !strings.Contains(body, "| cert-manager |") {
		t.Error("expected cert-manager in table")
	}
	if !strings.Contains(body, "| nginx |") {
		t.Error("expected nginx in table")
	}
	if !strings.Contains(body, "1.13.2 → 1.14.1") {
		t.Error("expected version range for cert-manager")
	}
	if !strings.Contains(body, "Release notes for cert-manager") {
		t.Error("expected release notes section for cert-manager")
	}
	if strings.Contains(body, "Release notes for nginx") {
		t.Error("did not expect release notes section for nginx (no notes)")
	}
	if !strings.Contains(body, "Close this PR") {
		t.Error("expected argoiax commands in footer")
	}
}

func TestRenderGroupPRBody_BreakingChanges(t *testing.T) {
	group := UpdateGroup{
		Updates: []UpdateInfo{
			{
				ChartName:       "grafana",
				OldVersion:      "7.0.1",
				NewVersion:      "8.0.0",
				FilePath:        "apps/monitoring.yaml",
				RepoURL:         "https://grafana.github.io/helm-charts",
				IsBreaking:      true,
				BreakingReasons: []string{"Major version bump detected"},
			},
			{
				ChartName:  "prometheus",
				OldVersion: "25.0.0",
				NewVersion: "25.1.0",
				FilePath:   "apps/monitoring.yaml",
				RepoURL:    "https://prometheus-community.github.io/helm-charts",
			},
		},
		Files: []FileUpdate{{FilePath: "apps/monitoring.yaml"}},
	}

	body := RenderGroupPRBody(group)

	if !strings.Contains(body, "> [!WARNING]") {
		t.Error("expected breaking change warning")
	}
	if !strings.Contains(body, "**grafana** 7.0.1 → 8.0.0") {
		t.Error("expected grafana in breaking changes list")
	}
	if !strings.Contains(body, "Major version bump detected") {
		t.Error("expected breaking reason")
	}
	// prometheus should not be in the breaking changes section
	if strings.Contains(body, "**prometheus**") {
		t.Error("did not expect prometheus in breaking changes warning")
	}
}

func TestRenderPRBody_DatadogReleaseNotes(t *testing.T) {
	// Simulates the Datadog scenario: chart from helm.datadoghq.com,
	// GitHub repo discovered via ArtifactHub content_url → DataDog/helm-charts.
	info := UpdateInfo{
		ChartName:  "datadog",
		RepoURL:    "https://helm.datadoghq.com",
		OldVersion: "3.180.0",
		NewVersion: "3.181.1",
		ReleaseNotes: &releasenotes.Notes{
			Entries: []releasenotes.Entry{
				{
					Version: "3.181.1",
					Body:    "## What's Changed\n* Bump Datadog Agent to 7.62.1\n* Fix APM socket path",
					URL:     "https://github.com/DataDog/helm-charts/releases/tag/datadog-3.181.1",
				},
				{
					Version: "3.181.0",
					Body:    "## What's Changed\n* Add Windows container support",
					URL:     "https://github.com/DataDog/helm-charts/releases/tag/datadog-3.181.0",
				},
			},
			SourceURL: "https://github.com/DataDog/helm-charts/releases",
		},
	}

	body := RenderPRBody(&info)

	checks := []struct {
		desc    string
		substr  string
		present bool
	}{
		{"opening line", "Bumps [datadog](https://helm.datadoghq.com) from 3.180.0 to 3.181.1", true},
		{"collapsible section", "<details>", true},
		{"release notes summary", "<summary>Release notes</summary>", true},
		{"source link", `<a href="https://github.com/DataDog/helm-charts/releases">datadog's releases</a>`, true},
		{"version header 3.181.1", "<h2>3.181.1</h2>", true},
		{"version header 3.181.0", "<h2>3.181.0</h2>", true},
		{"agent bump note", "Bump Datadog Agent to 7.62.1", true},
		{"APM fix note", "Fix APM socket path", true},
		{"Windows note", "Add Windows container support", true},
		{"blockquote wrapper", "<blockquote>", true},
		{"closing details", "</details>", true},
	}
	for _, c := range checks {
		if got := strings.Contains(body, c.substr); got != c.present {
			t.Errorf("%s: Contains(%q) = %v, want %v\nbody:\n%s", c.desc, c.substr, got, c.present, body)
		}
	}
}

func TestRenderPRBody_KubernetesAutoscalerReleaseNotes(t *testing.T) {
	// Simulates the Kubernetes autoscaler scenario: chart from
	// kubernetes.github.io/autoscaler, mapped to kubernetes/autoscaler,
	// tag pattern cluster-autoscaler-chart-9.46.0.
	info := UpdateInfo{
		ChartName:  "cluster-autoscaler",
		RepoURL:    "https://kubernetes.github.io/autoscaler",
		OldVersion: "9.43.2",
		NewVersion: "9.46.0",
		ReleaseNotes: &releasenotes.Notes{
			Entries: []releasenotes.Entry{
				{
					Version: "9.46.0",
					Body:    "## What's Changed\n* Bump cluster-autoscaler to 1.32.0\n* Update GKE defaults",
					URL:     "https://github.com/kubernetes/autoscaler/releases/tag/cluster-autoscaler-chart-9.46.0",
				},
			},
			SourceURL: "https://github.com/kubernetes/autoscaler/releases",
		},
	}

	body := RenderPRBody(&info)

	checks := []struct {
		desc    string
		substr  string
		present bool
	}{
		{"opening line", "Bumps [cluster-autoscaler](https://kubernetes.github.io/autoscaler) from 9.43.2 to 9.46.0", true},
		{"collapsible section", "<details>", true},
		{"release notes summary", "<summary>Release notes</summary>", true},
		{"source link", `<a href="https://github.com/kubernetes/autoscaler/releases">cluster-autoscaler's releases</a>`, true},
		{"version header", "<h2>9.46.0</h2>", true},
		{"autoscaler bump note", "Bump cluster-autoscaler to 1.32.0", true},
		{"GKE defaults note", "Update GKE defaults", true},
		{"blockquote wrapper", "<blockquote>", true},
	}
	for _, c := range checks {
		if got := strings.Contains(body, c.substr); got != c.present {
			t.Errorf("%s: Contains(%q) = %v, want %v\nbody:\n%s", c.desc, c.substr, got, c.present, body)
		}
	}
}

func TestRenderGroupPRBody_MixedReleaseNotes(t *testing.T) {
	group := UpdateGroup{
		Updates: []UpdateInfo{
			{
				ChartName:  "chart-a",
				OldVersion: "1.0.0",
				NewVersion: "1.1.0",
				FilePath:   "apps/a.yaml",
				RepoURL:    "https://example.com/a",
				ReleaseNotes: &releasenotes.Notes{
					Entries:   []releasenotes.Entry{{Version: "1.1.0", Body: "- New feature A"}},
					SourceURL: "https://github.com/example/a/releases",
				},
			},
			{
				ChartName:  "chart-b",
				OldVersion: "2.0.0",
				NewVersion: "2.1.0",
				FilePath:   "apps/b.yaml",
				RepoURL:    "https://example.com/b",
			},
			{
				ChartName:  "chart-c",
				OldVersion: "3.0.0",
				NewVersion: "3.1.0",
				FilePath:   "apps/c.yaml",
				RepoURL:    "https://example.com/c",
				ReleaseNotes: &releasenotes.Notes{
					Entries:   []releasenotes.Entry{{Version: "3.1.0", Body: "- Bugfix C"}},
					SourceURL: "https://github.com/example/c/releases",
				},
			},
		},
		Files: []FileUpdate{
			{FilePath: "apps/a.yaml"},
			{FilePath: "apps/b.yaml"},
			{FilePath: "apps/c.yaml"},
		},
	}

	body := RenderGroupPRBody(group)

	if !strings.Contains(body, "Release notes for chart-a") {
		t.Error("expected release notes for chart-a")
	}
	if strings.Contains(body, "Release notes for chart-b") {
		t.Error("did not expect release notes for chart-b")
	}
	if !strings.Contains(body, "Release notes for chart-c") {
		t.Error("expected release notes for chart-c")
	}
	if !strings.Contains(body, "New feature A") {
		t.Error("expected chart-a release notes content")
	}
	if !strings.Contains(body, "Bugfix C") {
		t.Error("expected chart-c release notes content")
	}
}
