package semver

import (
	"reflect"
	"testing"
)

func TestLatestStable(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "2.0.0", "1.2.3", "0.9.0"}

	latest, err := LatestStable(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "2.0.0" {
		t.Errorf("expected 2.0.0, got %s", latest)
	}
}

func TestLatestStable_WithConstraint(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "2.0.0", "1.2.3", "0.9.0"}

	latest, err := LatestStable(versions, ">=1.0.0, <2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "1.2.3" {
		t.Errorf("expected 1.2.3, got %s", latest)
	}
}

func TestLatestStable_WithVPrefix(t *testing.T) {
	versions := []string{"v1.0.0", "v1.1.0", "v2.0.0"}

	latest, err := LatestStable(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %s", latest)
	}
}

func TestLatestStable_EmptyList(t *testing.T) {
	latest, err := LatestStable([]string{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "" {
		t.Errorf("expected empty string, got %s", latest)
	}
}

func TestLatestStable_SkipsPreRelease(t *testing.T) {
	versions := []string{"1.0.0", "2.0.0-beta.1", "1.5.0"}

	latest, err := LatestStable(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "1.5.0" {
		t.Errorf("expected 1.5.0 (skipping pre-release), got %s", latest)
	}
}

func TestLatestStable_InvalidConstraint(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}

	_, err := LatestStable(versions, "not a valid constraint!!!")
	if err == nil {
		t.Error("expected error for invalid constraint string")
	}
}

func TestIsMajorBump(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"1.0.0", "2.0.0", true},
		{"1.5.3", "2.0.0", true},
		{"1.0.0", "1.1.0", false},
		{"1.0.0", "1.0.1", false},
		{"v1.0.0", "v2.0.0", true},
	}

	for _, tt := range tests {
		if got := IsMajorBump(tt.current, tt.latest); got != tt.want {
			t.Errorf("IsMajorBump(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "v1.0.0", true},
		{"1.0.0", "1.0.0", true},
		{"1.0.0", "1.1.0", false},
		{"v2.0.0", "v2.0.0", true},
		{"invalid", "invalid", true},
		{"invalid", "other", false},
		{"1.0.0", "invalid", false},
		{"invalid", "1.0.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			if got := Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("Equal(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestParsePair_EdgeCases(t *testing.T) {
	// Invalid current
	_, _, err := parsePair("not-a-version", "1.0.0")
	if err == nil {
		t.Error("expected error for invalid current version")
	}

	// Invalid latest
	_, _, err = parsePair("1.0.0", "not-a-version")
	if err == nil {
		t.Error("expected error for invalid latest version")
	}

	// Both valid
	cur, lat, err := parsePair("1.0.0", "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cur.Original() != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", cur.Original())
	}
	if lat.Original() != "2.0.0" {
		t.Errorf("expected 2.0.0, got %s", lat.Original())
	}
}

func TestIsMajorBump_EdgeCases(t *testing.T) {
	// Invalid versions should return false
	if IsMajorBump("invalid", "2.0.0") {
		t.Error("expected false for invalid current version")
	}
	if IsMajorBump("1.0.0", "invalid") {
		t.Error("expected false for invalid latest version")
	}
}

func TestVersionsBetween_EdgeCases(t *testing.T) {
	// Invalid versions
	result := VersionsBetween([]string{"1.0.0"}, "invalid", "2.0.0")
	if result != nil {
		t.Errorf("expected nil for invalid current, got %v", result)
	}

	// Empty list
	result = VersionsBetween([]string{}, "1.0.0", "2.0.0")
	if len(result) != 0 {
		t.Errorf("expected empty result for empty list, got %v", result)
	}

	// No versions between
	result = VersionsBetween([]string{"0.5.0", "3.0.0"}, "1.0.0", "2.0.0")
	if len(result) != 0 {
		t.Errorf("expected no versions between, got %v", result)
	}
}

func TestVersionsBetween_SkipsInvalid(t *testing.T) {
	// List includes invalid version strings that should be skipped
	versions := []string{"1.0.0", "invalid", "1.5.0", "also-bad", "2.0.0"}
	between := VersionsBetween(versions, "1.0.0", "2.0.0")
	if len(between) != 1 || between[0] != "1.5.0" {
		t.Errorf("expected [1.5.0], got %v", between)
	}
}

func TestLatestStable_AllInvalid(t *testing.T) {
	versions := []string{"invalid", "not-a-version", "garbage"}
	latest, err := LatestStable(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "" {
		t.Errorf("expected empty string for all invalid versions, got %s", latest)
	}
}

func TestVersionsBetween(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "2.0.0"}

	between := VersionsBetween(versions, "1.0.0", "2.0.0")
	expected := []string{"1.1.0", "1.2.0", "1.3.0"}

	if !reflect.DeepEqual(between, expected) {
		t.Errorf("expected %v, got %v", expected, between)
	}
}
