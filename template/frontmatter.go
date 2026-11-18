package template

import "strings"

// FrontMatter returns the keys of a file's front matter, and an empty map
// for a file that has none. It is for a caller that wants the keys alone;
// ProcessTemplate returns them with the expanded text.
func FrontMatter(content string) map[string]string {
	_, fm := stripFrontMatter(content)
	return fm
}

// stripFrontMatter extracts the front matter (lines of key: value between
// --- delimiters) and returns the remaining body and the pairs. A value is
// the text after the first colon, trimmed, as it is written: nothing in it
// is evaluated, and it is not YAML (quotes stay, there are no lists).
//
// The front matter is replaced by a template comment spanning the same number
// of lines, so that the line numbers in template parse and execution errors
// match the file. The comment emits nothing, so the expanded output is the
// same as if the front matter were simply removed.
func stripFrontMatter(content string) (body string, fm map[string]string) {
	fm = make(map[string]string)

	if !strings.HasPrefix(content, "---") {
		return content, fm
	}

	// Find the closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content, fm
	}

	block := rest[:idx]
	body = rest[idx+4:] // skip "\n---"
	// Strip leading newline from body
	body = strings.TrimPrefix(body, "\n")
	// The front matter occupied the two --- lines plus one line per newline
	// in the block (which starts with the newline after the opening ---).
	lines := strings.Count(block, "\n") + 2
	body = "{{/*" + strings.Repeat("\n", lines) + "*/}}" + body

	// Parse simple key: value pairs
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			fm[key] = val
		}
	}

	return body, fm
}
