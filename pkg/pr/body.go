package pr

import (
	"fmt"
	"strings"
)

// RenderPRBody generates a Dependabot-style PR body for a chart update.
func RenderPRBody(info UpdateInfo) string {
	var b strings.Builder

	// Opening line (Dependabot style)
	b.WriteString(fmt.Sprintf("Bumps [%s](%s) from %s to %s.\n", info.ChartName, info.RepoURL, info.OldVersion, info.NewVersion))

	// Breaking change warning
	if info.IsBreaking {
		b.WriteString("\n> [!WARNING]\n> This is a **major version update** and may contain breaking changes.\n")
		if len(info.BreakingReasons) > 0 {
			b.WriteString(">\n")
			for _, reason := range info.BreakingReasons {
				b.WriteString(fmt.Sprintf("> - %s\n", reason))
			}
		}
	}

	// Release notes section
	if info.ReleaseNotes != nil && len(info.ReleaseNotes.Entries) > 0 {
		b.WriteString("\n<details>\n<summary>Release notes</summary>\n")
		if info.ReleaseNotes.SourceURL != "" {
			b.WriteString(fmt.Sprintf("<p><em>Sourced from <a href=\"%s\">%s's releases</a>.</em></p>\n", info.ReleaseNotes.SourceURL, info.ChartName))
		}
		b.WriteString("<blockquote>\n")
		for _, entry := range info.ReleaseNotes.Entries {
			b.WriteString(fmt.Sprintf("<h2>%s</h2>\n", entry.Version))
			b.WriteString(entry.Body)
			b.WriteString("\n")
		}
		b.WriteString("</blockquote>\n</details>\n")
	}

	// Breaking change badge
	b.WriteString("\n<br />\n\n")
	if info.IsBreaking {
		if info.ReleaseNotes != nil && info.ReleaseNotes.SourceURL != "" {
			b.WriteString(fmt.Sprintf("[![Breaking change](https://img.shields.io/badge/change-breaking-red)](%s)\n\n", info.ReleaseNotes.SourceURL))
		} else {
			b.WriteString("![Breaking change](https://img.shields.io/badge/change-breaking-red)\n\n")
		}
	}

	// Footer
	b.WriteString("argoiax will resolve any conflicts with this PR as long as you don't alter it yourself. You can also trigger a recheck by commenting `@argoiax recheck`.\n")
	b.WriteString("\n---\n\n")
	b.WriteString("<details>\n<summary>argoiax commands and options</summary>\n<br />\n\n")
	b.WriteString("You can trigger argoiax actions by commenting on this PR:\n")
	b.WriteString("- `@argoiax recheck` will re-run the version check for this chart\n")
	b.WriteString("- `@argoiax ignore this major version` will close this PR and stop creating PRs for this major version\n")
	b.WriteString("- `@argoiax ignore this minor version` will close this PR and stop creating PRs for this minor version\n")
	b.WriteString("- `@argoiax ignore this chart` will close this PR and stop creating PRs for this chart\n")
	b.WriteString("\n</details>\n")

	return b.String()
}
