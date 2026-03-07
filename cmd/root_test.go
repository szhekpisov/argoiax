package cmd

import "testing"

func TestNewRootCmd(t *testing.T) {
	cmd := newRootCmd()

	if cmd.Use != "argoiax" {
		t.Errorf("expected Use argoiax, got %s", cmd.Use)
	}

	// Verify subcommands exist
	subCmds := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subCmds[sub.Use] = true
	}
	for _, name := range []string{"scan", "update", "version"} {
		if !subCmds[name] {
			t.Errorf("expected subcommand %q to be registered", name)
		}
	}

	// Verify persistent flags
	for _, flag := range []string{"config", "dir", "dry-run", "log-level"} {
		if cmd.PersistentFlags().Lookup(flag) == nil {
			t.Errorf("expected persistent flag %q to be registered", flag)
		}
	}
}

func TestSetupLogging(t *testing.T) {
	// These should not panic
	levels := []string{"debug", "info", "warn", "error", "invalid-level"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			setupLogging(level) // should not panic
		})
	}
}
