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

func TestVersionsBetween(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "2.0.0"}

	between := VersionsBetween(versions, "1.0.0", "2.0.0")
	expected := []string{"1.1.0", "1.2.0", "1.3.0"}

	if !reflect.DeepEqual(between, expected) {
		t.Errorf("expected %v, got %v", expected, between)
	}
}
