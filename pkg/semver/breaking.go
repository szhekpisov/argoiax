package semver

import (
	"fmt"
	"regexp"
	"strings"
)

// BreakingChangeResult contains the analysis of potential breaking changes.
type BreakingChangeResult struct {
	IsBreaking bool
	Reasons    []string
}

const maxBreakingReasons = 5

var breakingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bBREAKING\s+CHANGES?\b`),
	regexp.MustCompile(`(?i)\bBREAKING\b:\s`),
	regexp.MustCompile(`(?i)\bremoved\b.*\b(?:parameter|field|option|flag|api|endpoint|feature)\b`),
	regexp.MustCompile(`(?i)\b(?:parameter|field|option|flag|api|endpoint|feature)\b.*\bremoved\b`),
	regexp.MustCompile(`(?i)\bincompatible\b`),
	regexp.MustCompile(`(?i)\b(?:no longer|not)\s+(?:supported|compatible|available)\b`),
	regexp.MustCompile(`(?i)\bmigrat(?:e|ion)\s+(?:required|needed|necessary)\b`),
}

// DetectBreaking checks for breaking changes between two versions.
// It combines semver analysis with content-based scanning of release notes.
func DetectBreaking(current, latest, releaseNotesBody string) BreakingChangeResult {
	result := BreakingChangeResult{}

	if IsMajorBump(current, latest) {
		result.IsBreaking = true
		result.Reasons = append(result.Reasons, "Major version bump detected")
	}

	if releaseNotesBody != "" {
		for line := range strings.SplitSeq(releaseNotesBody, "\n") {
			if len(result.Reasons) >= maxBreakingReasons-1 {
				result.Reasons = append(result.Reasons, fmt.Sprintf("... and more (capped at %d)", maxBreakingReasons))
				break
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			for _, pattern := range breakingPatterns {
				if pattern.MatchString(trimmed) {
					result.IsBreaking = true
					result.Reasons = append(result.Reasons, trimmed)
					break
				}
			}
		}
	}

	return result
}
