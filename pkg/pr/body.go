package pr

import (
	"fmt"
	"html"
	"strings"

	"github.com/szhekpisov/argoiax/pkg/releasenotes"
)

// RenderPRBody generates a Dependabot-style PR body for a chart update.
func RenderPRBody(info *UpdateInfo) string {
	var b strings.Builder

	// Opening line (Dependabot style)
	fmt.Fprintf(&b, "Bumps [%s](%s) from %s to %s.\n", info.ChartName, info.RepoURL, info.OldVersion, info.NewVersion)

	// Breaking change warning
	if info.IsBreaking {
		b.WriteString("\n> [!WARNING]\n> This is a **major version update** and may contain breaking changes.\n")
		if len(info.BreakingReasons) > 0 {
			b.WriteString(">\n")
			for _, reason := range info.BreakingReasons {
				fmt.Fprintf(&b, "> - %s\n", reason)
			}
		}
	}

	// Release notes section
	if info.ReleaseNotes != nil && len(info.ReleaseNotes.Entries) > 0 {
		writeReleaseNotes(&b, info.ChartName, info.ReleaseNotes, "Release notes")
	}

	// Breaking change badge
	b.WriteString("\n<br />\n\n")
	if info.IsBreaking {
		if info.ReleaseNotes != nil && info.ReleaseNotes.SourceURL != "" {
			fmt.Fprintf(&b, "[![Breaking change](https://img.shields.io/badge/change-breaking-red)](%s)\n\n", info.ReleaseNotes.SourceURL)
		} else {
			b.WriteString("![Breaking change](https://img.shields.io/badge/change-breaking-red)\n\n")
		}
	}

	// Footer
	writeFooter(&b, []string{
		"- Close this PR to stop argoiax from recreating it",
	})

	return b.String()
}

// RenderGroupPRBody generates a PR body for a group of chart updates.
func RenderGroupPRBody(group UpdateGroup) string {
	var b strings.Builder

	// Summary table
	b.WriteString("## Updated Charts\n\n")
	b.WriteString("| Chart | File | Version |\n")
	b.WriteString("|-------|------|---------|\n")
	for _, u := range group.Updates {
		fmt.Fprintf(&b, "| %s | `%s` | %s → %s |\n", u.ChartName, u.FilePath, u.OldVersion, u.NewVersion)
	}

	// Breaking change warnings
	var breakingUpdates []UpdateInfo
	for _, u := range group.Updates {
		if u.IsBreaking {
			breakingUpdates = append(breakingUpdates, u)
		}
	}
	if len(breakingUpdates) > 0 {
		b.WriteString("\n> [!WARNING]\n> This PR contains **major version updates** that may include breaking changes:\n>\n")
		for _, u := range breakingUpdates {
			fmt.Fprintf(&b, "> - **%s** %s → %s\n", u.ChartName, u.OldVersion, u.NewVersion)
			for _, reason := range u.BreakingReasons {
				fmt.Fprintf(&b, ">   - %s\n", reason)
			}
		}
	}

	// Per-chart release notes
	for _, u := range group.Updates {
		if u.ReleaseNotes == nil || len(u.ReleaseNotes.Entries) == 0 {
			continue
		}
		summary := fmt.Sprintf("Release notes for %s (%s → %s)", u.ChartName, u.OldVersion, u.NewVersion)
		writeReleaseNotes(&b, u.ChartName, u.ReleaseNotes, summary)
	}

	// Footer
	b.WriteString("\n<br />\n\n")
	writeFooter(&b, []string{
		"- Close this PR to stop argoiax from recreating it",
	})

	return b.String()
}

// writeReleaseNotes writes a collapsible release notes section into the builder.
func writeReleaseNotes(b *strings.Builder, chartName string, notes *releasenotes.Notes, summary string) {
	fmt.Fprintf(b, "\n<details>\n<summary>%s</summary>\n", summary)
	if notes.SourceURL != "" {
		fmt.Fprintf(b, "<p><em>Sourced from <a href=\"%s\">%s's releases</a>.</em></p>\n", notes.SourceURL, html.EscapeString(chartName))
	}
	b.WriteString("<blockquote>\n")
	for _, entry := range notes.Entries {
		fmt.Fprintf(b, "<h2>%s</h2>\n", html.EscapeString(entry.Version))
		b.WriteString(html.EscapeString(entry.Body))
		b.WriteString("\n")
	}
	b.WriteString("</blockquote>\n</details>\n")
}

// writeFooter writes the standard PR footer with argoiax commands.
func writeFooter(b *strings.Builder, commands []string) {
	b.WriteString("argoiax will resolve any conflicts with this PR as long as you don't alter it yourself.\n")
	b.WriteString("\n---\n\n")
	b.WriteString("<details>\n<summary>argoiax commands and options</summary>\n<br />\n\n")
	b.WriteString("You can trigger argoiax actions by commenting on this PR:\n")
	for _, cmd := range commands {
		b.WriteString(cmd)
		b.WriteString("\n")
	}
	b.WriteString("\n</details>\n")
}
