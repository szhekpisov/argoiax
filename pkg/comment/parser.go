package comment

import "strings"

// Command represents a parsed command from a PR comment.
type Command struct {
	Name string
	Args []string
}

// supportedCommands lists the commands that argoiax recognizes.
var supportedCommands = []string{"rebase", "recreate"}

// Parse extracts an @argoiax command from a comment body.
// Returns nil if no mention is found (most comments aren't commands).
func Parse(body string) *Command {
	// Strip fenced code blocks (``` ... ```)
	stripped := stripFencedCodeBlocks(body)
	// Strip inline code (`...`)
	stripped = stripInlineCode(stripped)

	for _, line := range strings.Split(stripped, "\n") {
		if cmd := parseLine(line); cmd != nil {
			return cmd
		}
	}
	return nil
}

// SupportedCommands returns the list of supported command names.
func SupportedCommands() []string {
	return append([]string(nil), supportedCommands...)
}

func parseLine(line string) *Command {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, "@argoiax")
	if idx < 0 {
		return nil
	}

	// Extract the rest after "@argoiax"
	rest := strings.TrimSpace(line[idx+len("@argoiax"):])
	if rest == "" {
		return nil
	}

	fields := strings.Fields(rest)
	name := strings.ToLower(fields[0])

	var args []string
	if len(fields) > 1 {
		args = fields[1:]
	}

	return &Command{
		Name: name,
		Args: args,
	}
}

func stripFencedCodeBlocks(s string) string {
	var result strings.Builder
	lines := strings.Split(s, "\n")
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func stripInlineCode(s string) string {
	var result strings.Builder
	inCode := false
	for _, r := range s {
		if r == '`' {
			inCode = !inCode
			continue
		}
		if !inCode {
			result.WriteRune(r)
		}
	}
	return result.String()
}
