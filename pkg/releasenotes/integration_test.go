//go:build integration

package releasenotes_test

import (
	"context"
	"strings"
	"testing"

	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/pr"
	"github.com/szhekpisov/argoiax/pkg/releasenotes"
)

// These tests hit real GitHub and ArtifactHub APIs.
// Run with: go test -tags integration -count=1 ./pkg/releasenotes/...

func TestIntegration_DatadogReleaseNotes(t *testing.T) {
	cfg := config.ReleaseNotesConfig{
		Enabled:             true,
		MaxLength:           10000,
		IncludeIntermediate: true,
		Sources:             []string{config.SourceGitHubReleases, config.SourceArtifactHub, config.SourceChangelog},
	}

	o := releasenotes.NewOrchestrator(cfg, "")

	notes := o.FetchNotes(context.Background(), "datadog", "https://helm.datadoghq.com", []string{"3.181.1"}, nil)
	if notes == nil {
		t.Fatal("expected non-nil release notes for datadog chart")
	}
	if len(notes.Entries) == 0 {
		t.Fatal("expected at least one release notes entry")
	}

	t.Logf("source: %s", notes.SourceURL)
	for _, e := range notes.Entries {
		t.Logf("version %s: %d bytes, url: %s", e.Version, len(e.Body), e.URL)
	}

	// Verify it renders into a PR body with release notes HTML
	info := pr.UpdateInfo{
		ChartName:    "datadog",
		RepoURL:      "https://helm.datadoghq.com",
		OldVersion:   "3.180.0",
		NewVersion:   "3.181.1",
		ReleaseNotes: notes,
	}
	body := pr.RenderPRBody(&info)

	requireContains(t, body, "<details>", "collapsible section")
	requireContains(t, body, "<summary>Release notes</summary>", "release notes summary")
	requireContains(t, body, "<h2>3.181.1</h2>", "version header")
	requireContains(t, body, "<blockquote>", "blockquote")

	t.Logf("rendered PR body (%d bytes):\n%s", len(body), body)
}

func TestIntegration_KubernetesAutoscalerReleaseNotes(t *testing.T) {
	cfg := config.ReleaseNotesConfig{
		Enabled:             true,
		MaxLength:           10000,
		IncludeIntermediate: true,
		Sources:             []string{config.SourceGitHubReleases, config.SourceArtifactHub, config.SourceChangelog},
	}

	o := releasenotes.NewOrchestrator(cfg, "")

	notes := o.FetchNotes(context.Background(), "cluster-autoscaler", "https://kubernetes.github.io/autoscaler", []string{"9.46.0"}, nil)
	if notes == nil {
		t.Fatal("expected non-nil release notes for cluster-autoscaler chart")
	}
	if len(notes.Entries) == 0 {
		t.Fatal("expected at least one release notes entry")
	}

	t.Logf("source: %s", notes.SourceURL)
	for _, e := range notes.Entries {
		t.Logf("version %s: %d bytes, url: %s", e.Version, len(e.Body), e.URL)
	}

	// Verify it renders into a PR body with release notes HTML
	info := pr.UpdateInfo{
		ChartName:    "cluster-autoscaler",
		RepoURL:      "https://kubernetes.github.io/autoscaler",
		OldVersion:   "9.43.2",
		NewVersion:   "9.46.0",
		ReleaseNotes: notes,
	}
	body := pr.RenderPRBody(&info)

	requireContains(t, body, "<details>", "collapsible section")
	requireContains(t, body, "<summary>Release notes</summary>", "release notes summary")
	requireContains(t, body, "<h2>9.46.0</h2>", "version header")
	requireContains(t, body, "<blockquote>", "blockquote")
	requireContains(t, body, "kubernetes/autoscaler", "source repo link")

	t.Logf("rendered PR body (%d bytes):\n%s", len(body), body)
}

func TestIntegration_SealedSecretsReleaseNotes(t *testing.T) {
	cfg := config.ReleaseNotesConfig{
		Enabled:             true,
		MaxLength:           10000,
		IncludeIntermediate: true,
		Sources:             []string{config.SourceGitHubReleases, config.SourceArtifactHub, config.SourceChangelog},
	}

	o := releasenotes.NewOrchestrator(cfg, "")

	notes := o.FetchNotes(context.Background(), "sealed-secrets", "https://bitnami-labs.github.io/sealed-secrets", []string{"2.18.3"}, nil)
	if notes == nil {
		t.Fatal("expected non-nil release notes for sealed-secrets chart")
	}
	if len(notes.Entries) == 0 {
		t.Fatal("expected at least one release notes entry")
	}

	t.Logf("source: %s", notes.SourceURL)
	for _, e := range notes.Entries {
		t.Logf("version %s: %d bytes, url: %s", e.Version, len(e.Body), e.URL)
	}

	info := pr.UpdateInfo{
		ChartName:    "sealed-secrets",
		RepoURL:      "https://bitnami-labs.github.io/sealed-secrets",
		OldVersion:   "2.17.0",
		NewVersion:   "2.18.3",
		ReleaseNotes: notes,
	}
	body := pr.RenderPRBody(&info)

	requireContains(t, body, "<details>", "collapsible section")
	requireContains(t, body, "<h2>2.18.3</h2>", "version header")
	requireContains(t, body, "bitnami-labs/sealed-secrets", "source repo link")

	t.Logf("rendered PR body (%d bytes):\n%s", len(body), body)
}

func requireContains(t *testing.T, body, substr, desc string) {
	t.Helper()
	if !strings.Contains(body, substr) {
		t.Errorf("%s: expected %q in body", desc, substr)
	}
}
