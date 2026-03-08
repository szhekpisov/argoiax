package comment

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantCmd *Command
	}{
		{
			name:    "rebase command",
			body:    "@argoiax rebase",
			wantCmd: &Command{Name: "rebase"},
		},
		{
			name:    "recreate command",
			body:    "@argoiax recreate",
			wantCmd: &Command{Name: "recreate"},
		},
		{
			name:    "no mention",
			body:    "This is a regular comment",
			wantCmd: nil,
		},
		{
			name:    "empty body",
			body:    "",
			wantCmd: nil,
		},
		{
			name:    "mention without command",
			body:    "@argoiax",
			wantCmd: nil,
		},
		{
			name:    "unknown command",
			body:    "@argoiax deploy",
			wantCmd: &Command{Name: "deploy"},
		},
		{
			name:    "case insensitive mention",
			body:    "@Argoiax Rebase",
			wantCmd: &Command{Name: "rebase"},
		},
		{
			name:    "mixed case",
			body:    "@ARGOIAX RECREATE",
			wantCmd: &Command{Name: "recreate"},
		},
		{
			name:    "inside fenced code block",
			body:    "```\n@argoiax rebase\n```",
			wantCmd: nil,
		},
		{
			name:    "inside inline code",
			body:    "Use `@argoiax rebase` to rebase",
			wantCmd: nil,
		},
		{
			name:    "embedded in longer text",
			body:    "Hey team, can we @argoiax rebase this PR?",
			wantCmd: &Command{Name: "rebase", Args: []string{"this", "PR?"}},
		},
		{
			name:    "command on second line",
			body:    "Some preamble\n@argoiax recreate\nSome epilogue",
			wantCmd: &Command{Name: "recreate"},
		},
		{
			name:    "code block then real command",
			body:    "```\n@argoiax rebase\n```\n@argoiax recreate",
			wantCmd: &Command{Name: "recreate"},
		},
		{
			name:    "inline code then real command",
			body:    "Use `@argoiax rebase` or just:\n@argoiax recreate",
			wantCmd: &Command{Name: "recreate"},
		},
		{
			name:    "with extra args",
			body:    "@argoiax rebase --force",
			wantCmd: &Command{Name: "rebase", Args: []string{"--force"}},
		},
		{
			name:    "inside tilde fenced code block",
			body:    "~~~\n@argoiax rebase\n~~~",
			wantCmd: nil,
		},
		{
			name:    "tilde fence then real command",
			body:    "~~~\n@argoiax rebase\n~~~\n@argoiax recreate",
			wantCmd: &Command{Name: "recreate"},
		},
		{
			name:    "inside double backtick inline code",
			body:    "Use ``@argoiax rebase`` to rebase",
			wantCmd: nil,
		},
		{
			name:    "double backtick then real command",
			body:    "Use ``@argoiax rebase`` or:\n@argoiax recreate",
			wantCmd: &Command{Name: "recreate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.body)
			if tt.wantCmd == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected command, got nil")
			}
			if got.Name != tt.wantCmd.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantCmd.Name)
			}
			if len(got.Args) != len(tt.wantCmd.Args) {
				t.Errorf("Args = %v, want %v", got.Args, tt.wantCmd.Args)
			}
		})
	}
}

func TestSupportedCommands(t *testing.T) {
	cmds := SupportedCommands()
	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 supported commands, got %d", len(cmds))
	}
	want := map[string]bool{"rebase": true, "recreate": true}
	for _, c := range cmds {
		if !want[c] {
			t.Errorf("unexpected supported command: %s", c)
		}
	}
}
