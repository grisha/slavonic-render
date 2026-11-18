package template

//
// These are template functions, available from within templates via a FuncMap
//

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/grisha/slavonic-render/cu"
)

// stixPrefix is "сⷯ" (с + U+2DEF combining Cyrillic letter ha), the abbreviation
// for "стихъ" (verse) a Church Slavonic document writes before the first
// verse's number.
const stixPrefix = "с\u2DEF"

// blankLine matches one or more blank lines; a line holding only whitespace
// is blank, as in the markup parser.
var blankLine = regexp.MustCompile(`\n\s*\n`)

// paragraphs splits text into its paragraphs, the runs of lines separated by
// blank lines, as the markup parser sees them. Each paragraph is trimmed,
// internal line breaks are kept, empty paragraphs are dropped. The result is
// a list for the template builtins range, index, slice and len; printing the
// list itself renders it as [a b c].
//
// The returned slice has cap == len because the slice builtin bounds-checks
// against cap: otherwise an end past the last paragraph would yield empty
// strings instead of an error.
//
// Example:
//
//	{{ $t := include "canon/theotokos/ode1.md" | paragraphs }}
//	{{ range slice $t 0 2 }}
//	-~Прест҃а́ѧ бцⷣе, сп҃сѝ на́съ.
//
//	-{{ . }}
//	{{ end }}
func paragraphs(text string) []string {
	var paras []string
	for _, p := range blankLine.Split(text, -1) {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, p)
		}
	}
	return paras[:len(paras):len(paras)]
}

// numberVerses joins all verses (one paragraph each) into one continuous
// text block with &{margin ...} margin note markers for verse numbers.
//
// The first verse's numeral follows prefix and a space when there is a
// prefix (a Church Slavonic document's stixPrefix: &{margin сⷯ а҃}; an
// English one has none: &{margin 1}); all subsequent verses get bare
// numerals like &{margin в҃}, &{margin г҃}, etc., as write writes them (the
// document's numerals).
//
// Example output (for psalm 1, in Slavonic):
//
//	&{margin сⷯ а҃}Бл҃же́нъ мꙋ́жъ...сѣ́де: &{margin в҃}Но въ зако́нѣ...но́щь. &{margin г҃}И҆ бꙋ́детъ...
func numberVerses(write func(int) string, prefix, text string) string {
	var parts []string
	for i, para := range paragraphs(text) {
		para = strings.ReplaceAll(para, "\n", " ")
		num := write(i + 1)
		if i == 0 && prefix != "" {
			parts = append(parts, "&{margin "+prefix+" "+num+"}"+para)
		} else {
			parts = append(parts, "&{margin "+num+"}"+para)
		}
	}
	return strings.Join(parts, " ")
}

// verseRange extracts a 1-based inclusive range of verses from already-numbered
// text (output of numberVerses). It locates verse boundaries by counting
// &{margin markers — the Nth occurrence marks verse N. The piped text is the
// last argument; a range outside the text is an error.
//
// Example:
//
//	{{ include "psalms/118.md" | numberVerses | verseRange 73 131 }}
func verseRange(start, end int, text string) (string, error) {
	if start < 1 {
		return "", fmt.Errorf("verseRange: start must be at least 1, got %d", start)
	}
	if end < start {
		return "", fmt.Errorf("verseRange: end %d before start %d", end, start)
	}

	// Find all &{margin marker positions
	const marginPrefix = "&{margin "
	var positions []int
	searchFrom := 0
	for {
		idx := strings.Index(text[searchFrom:], marginPrefix)
		if idx < 0 {
			break
		}
		positions = append(positions, searchFrom+idx)
		searchFrom += idx + len(marginPrefix)
	}
	if len(positions) == 0 {
		return "", fmt.Errorf("verseRange: no verse markers in text; pipe through numberVerses first")
	}
	if end > len(positions) {
		return "", fmt.Errorf("verseRange: end %d past the last verse (%d)", end, len(positions))
	}

	// Verse N begins at positions[N-1] and ends just before positions[N],
	// or at the end of the text for the last verse.
	beginPos := positions[start-1]
	endPos := len(text)
	if end < len(positions) {
		endPos = positions[end]
	}
	return strings.TrimSpace(text[beginPos:endPos]), nil
}

// firstVerseAsHeading splits already-numbered verse text, turning the first N
// verses into a rubric heading (=...=). The remaining verses become the body.
//
// Optional arguments before the piped text, in any order: an int N, the
// number of verses in the heading (default 1), and "dropcap", which prefixes
// the body with ^ for drop-cap rendering. Anything else is an error.
//
// Examples:
//
//	{{ include "psalms/004.md" | numberVerses | firstVerseAsHeading "dropcap" }}
//	{{ include "psalms/050.md" | numberVerses | firstVerseAsHeading 2 "dropcap" }}
func firstVerseAsHeading(args ...any) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("firstVerseAsHeading: no text")
	}
	// Last argument is always the piped text
	text, ok := args[len(args)-1].(string)
	if !ok {
		return "", fmt.Errorf("firstVerseAsHeading: expected text as the last argument, got %T", args[len(args)-1])
	}
	dropCap := false
	verseCount := 1
	for _, a := range args[:len(args)-1] {
		switch v := a.(type) {
		case int:
			if v < 1 {
				return "", fmt.Errorf("firstVerseAsHeading: verse count must be positive, got %d", v)
			}
			verseCount = v
		case string:
			if v != "dropcap" {
				return "", fmt.Errorf("firstVerseAsHeading: unknown option %q", v)
			}
			dropCap = true
		default:
			return "", fmt.Errorf("firstVerseAsHeading: unexpected argument %v (%T)", a, a)
		}
	}

	// Skip past verseCount &{margin ...} markers to find the split point
	const marginPrefix = "&{margin "
	pos := 0
	for i := 0; i < verseCount; i++ {
		marker := strings.Index(text[pos:], marginPrefix)
		if marker < 0 {
			// Fewer markers than requested — entire text is heading
			m, rest := splitLeadingDirectives(strings.TrimSpace(text))
			return m + "|=" + rest + "=", nil
		}
		closing := strings.Index(text[pos+marker:], "}")
		if closing < 0 {
			m, rest := splitLeadingDirectives(strings.TrimSpace(text))
			return m + "|=" + rest + "=", nil
		}
		pos = pos + marker + closing + 1
	}

	// Find the next &{margin marker — that's where the body starts
	nextMarker := strings.Index(text[pos:], marginPrefix)
	if nextMarker < 0 {
		// All verses consumed by heading
		m, rest := splitLeadingDirectives(strings.TrimSpace(text))
		return m + "|=" + rest + "=", nil
	}
	splitPos := pos + nextMarker

	heading := strings.TrimSpace(text[:splitPos])
	body := strings.TrimSpace(text[splitPos:])

	// Place &{...} markers before structural prefixes (|=, ^) so that
	// verseRange can slice correctly — it starts extraction at the &{
	// position, so any prefix before it would be lost.
	// The paragraph transformer in markup/parsers.go skips leading &{...}
	// directives before checking for |, ^, etc.
	markers, headingRest := splitLeadingDirectives(heading)
	result := markers + "|=" + headingRest + "=\n\n"
	if dropCap {
		markers, bodyRest := splitLeadingDirectives(body)
		result += markers + "^" + bodyRest
	} else {
		result += body
	}
	return result, nil
}

// splitLeadingDirectives splits off any leading &{...} directives from s,
// returning them as a prefix and the remaining text.
func splitLeadingDirectives(s string) (directives, rest string) {
	for strings.HasPrefix(s, "&{") {
		end := strings.Index(s, "}")
		if end < 0 {
			break
		}
		directives += s[:end+1]
		s = s[end+1:]
	}
	return directives, s
}

// set sets a data key for everything expanded after it: the rest of the
// file, the files included from there on, and the renderers, which read
// their keys (DocumentTitle, Numerals) when the expansion is done. It writes
// nothing.
//
// Usage: {{ set "Tone" "5" }}
func (tp *templateProcessor) set(key, value string) string {
	if fm, ok := tp.data.(map[string]string); ok {
		fm[key] = value
	}
	return ""
}

// hole is what text/template leaves where a key is not set: «<no value>»
// for {{ .Key }}, and for a printf given one «%!s(<nil>)» or another of
// fmt's complaints, which are all «%!», a verb or none, and a parenthesis
// («%!d(string=…)», «%!(EXTRA …)»). A «%!» of the text itself («100%! off»)
// is not one.
var hole = regexp.MustCompile(`<no value>|%!.?\(`)

// findHole is an error quoting the first line of the expanded text that
// holds a hole and counting the lines that do, so that a key not set never
// reaches the page.
func findHole(text string) error {
	var first string
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if hole.MatchString(line) {
			if count == 0 {
				first = strings.TrimSpace(line)
			}
			count++
		}
	}
	if count == 0 {
		return nil
	}
	if len([]rune(first)) > 120 {
		first = string([]rune(first)[:120]) + "…"
	}
	return fmt.Errorf("findHole: a key that is not set, in %d line(s) of the expanded text, the first: %s", count, first)
}

// numMarker is a number marked in a text, &{num сл҃г} or &{num 233}, with
// the backslashes before it.
var numMarker = regexp.MustCompile(`(\\*)&\{num\s+([^}]*)\}`)

// expandNum writes every &{num …} marker in text as write writes the
// number it marks; a marker that is not a number is an error quoting it.
// A marker inside another directive (&{margin На &{num і҃}:}) has no brace
// of its own, so it is matched and written first. A marker whose & is
// escaped (an odd number of backslashes before it) is text, and is left
// for the markup to print.
func expandNum(write func(int) string, text string) (string, error) {
	var err error
	out := numMarker.ReplaceAllStringFunc(text, func(m string) string {
		sub := numMarker.FindStringSubmatch(m)
		if len(sub[1])%2 == 1 {
			return m
		}
		n, perr := cu.ParseNumeral(strings.TrimSpace(sub[2]))
		if perr != nil && err == nil {
			err = fmt.Errorf("expandNum: %s: %w", m[len(sub[1]):], perr)
		}
		return sub[1] + write(n)
	})
	return out, err
}
