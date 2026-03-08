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

func isFenceDelimiter(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func stripFencedCodeBlocks(s string) string {
	var result strings.Builder
	lines := strings.Split(s, "\n")
	inFence := false
	for _, line := range lines {
		if isFenceDelimiter(line) {
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
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '`' {
			// Count consecutive backticks to find the delimiter length.
			delimLen := 0
			for i+delimLen < len(runes) && runes[i+delimLen] == '`' {
				delimLen++
			}
			// Search for a matching closing delimiter of the same length.
			j := i + delimLen
			found := false
			for j <= len(runes)-delimLen {
				if runes[j] == '`' {
					closeLen := 0
					for j+closeLen < len(runes) && runes[j+closeLen] == '`' {
						closeLen++
					}
					if closeLen == delimLen {
						// Skip past the closing delimiter.
						i = j + closeLen
						found = true
						break
					}
					j += closeLen
				} else {
					j++
				}
			}
			if !found {
				// No matching closer — emit the backticks as literal text.
				for k := 0; k < delimLen; k++ {
					result.WriteRune('`')
				}
				i += delimLen
			}
		} else {
			result.WriteRune(runes[i])
			i++
		}
	}
	return result.String()
}
