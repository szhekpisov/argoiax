package semver

import "testing"

func TestDetectBreaking_MajorBump(t *testing.T) {
	result := DetectBreaking("1.0.0", "2.0.0", "")
	if !result.IsBreaking {
		t.Error("expected breaking for major bump")
	}
	if len(result.Reasons) == 0 {
		t.Error("expected at least one reason")
	}
}

func TestDetectBreaking_MinorBump(t *testing.T) {
	result := DetectBreaking("1.0.0", "1.1.0", "")
	if result.IsBreaking {
		t.Error("expected non-breaking for minor bump without breaking content")
	}
}

func TestDetectBreaking_ContentBased(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    bool
	}{
		{"breaking change keyword", "BREAKING CHANGE: removed old API", true},
		{"removed parameter", "The deprecated parameter has been removed", true},
		{"incompatible", "This version is incompatible with v1", true},
		{"migration required", "Migration required for this update", true},
		{"no longer supported", "Feature X is no longer supported", true},
		{"normal changelog", "Added new feature\nFixed bug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectBreaking("1.0.0", "1.1.0", tt.body)
			if result.IsBreaking != tt.want {
				t.Errorf("DetectBreaking content %q: got %v, want %v", tt.name, result.IsBreaking, tt.want)
			}
		})
	}
}

func TestDetectBreaking_CombinedMajorAndContent(t *testing.T) {
	result := DetectBreaking("1.0.0", "2.0.0", "BREAKING CHANGE: removed old API")
	if !result.IsBreaking {
		t.Error("expected breaking")
	}
	if len(result.Reasons) < 2 {
		t.Errorf("expected at least 2 reasons, got %d", len(result.Reasons))
	}
}

func TestDetectBreaking_ReasonsCapped(t *testing.T) {
	body := `BREAKING CHANGE: first
BREAKING CHANGE: second
BREAKING CHANGE: third
BREAKING CHANGE: fourth
BREAKING CHANGE: fifth
BREAKING CHANGE: sixth`

	result := DetectBreaking("1.0.0", "1.1.0", body)
	if !result.IsBreaking {
		t.Error("expected breaking")
	}
	if len(result.Reasons) > 5 {
		t.Errorf("expected at most 5 reasons, got %d", len(result.Reasons))
	}
	last := result.Reasons[len(result.Reasons)-1]
	if last != "... and more (capped at 5)" {
		t.Errorf("expected overflow message, got %q", last)
	}
}
