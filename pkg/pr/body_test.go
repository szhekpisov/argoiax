package pr

import (
	"strings"
	"testing"

	"github.com/vertrost/ancaeus/pkg/releasenotes"
)

func TestRenderPRBody_Basic(t *testing.T) {
	info := UpdateInfo{
		ChartName:  "cert-manager",
		RepoURL:   "https://charts.jetstack.io",
		OldVersion: "1.13.2",
		NewVersion: "1.14.1",
		IsBreaking: false,
	}

	body := RenderPRBody(info)

	if !strings.Contains(body, "Bumps [cert-manager](https://charts.jetstack.io) from 1.13.2 to 1.14.1.") {
		t.Error("expected Dependabot-style opening line")
	}
	if strings.Contains(body, "WARNING") {
		t.Error("did not expect breaking change warning")
	}
	if !strings.Contains(body, "@ancaeus recheck") {
		t.Error("expected ancaeus commands in footer")
	}
	if !strings.Contains(body, "---") {
		t.Error("expected separator before footer")
	}
}

func TestRenderPRBody_Breaking(t *testing.T) {
	info := UpdateInfo{
		ChartName:       "grafana",
		RepoURL:        "https://grafana.github.io/helm-charts",
		OldVersion:      "7.0.1",
		NewVersion:      "8.2.0",
		IsBreaking:      true,
		BreakingReasons: []string{"Major version bump detected"},
		ReleaseNotes: &releasenotes.Notes{
			SourceURL: "https://github.com/grafana/helm-charts/releases",
		},
	}

	body := RenderPRBody(info)

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
		RepoURL:   "https://charts.jetstack.io",
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

	body := RenderPRBody(info)

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

	body := RenderPRBody(info)

	if strings.Contains(body, "<summary>Release notes</summary>") {
		t.Error("did not expect release notes section")
	}
	if !strings.Contains(body, "Bumps [test-chart](https://example.com)") {
		t.Error("expected opening line")
	}
}
