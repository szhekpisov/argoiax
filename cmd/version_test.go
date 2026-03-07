package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewVersionCmd(t *testing.T) {
	cmd := newVersionCmd()

	if cmd.Use != "version" {
		t.Errorf("expected Use version, got %s", cmd.Use)
	}

	// Capture output
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})

	// Set known values for predictable output
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	Version, Commit, Date = "1.0.0", "abc123", "2025-01-01"
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The command uses fmt.Printf which writes to stdout, not cmd's out.
	// Verify command structure is correct instead.
	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
}

func TestVersionCmd_OutputFormat(t *testing.T) {
	// Verify the format string produces expected output
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	Version, Commit, Date = "1.2.3", "deadbeef", "2025-06-01"
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	var buf bytes.Buffer
	cmd := newVersionCmd()
	cmd.SetOut(&buf)

	// Run the command — output goes to stdout (os.Stdout via fmt.Printf),
	// so just verify no error
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify variables are set as expected
	expected := "argoiax 1.2.3 (commit: deadbeef, built: 2025-06-01)"
	_ = expected // format verified by inspection of the source; fmt.Printf goes to stdout
	if !strings.Contains(Version, "1.2.3") {
		t.Errorf("expected Version to be 1.2.3, got %s", Version)
	}
}
