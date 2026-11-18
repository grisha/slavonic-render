package template

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/grisha/slavonic-render/cu"
)

// A front-matter value is the text after the colon, as it is written: an
// expression in it is not evaluated and brackets are not a list.
func TestStripFrontMatterVerbatim(t *testing.T) {
	input := "---\na: ${b}${c}\nb: [x, y]\nc: [Draft]\n---\nbody"
	body, fm := stripFrontMatter(input)
	// The five front-matter lines become a five-line template comment.
	if want := "{{/*\n\n\n\n\n*/}}body"; body != want {
		t.Errorf("unexpected body: %q, want %q", body, want)
	}
	for key, want := range map[string]string{"a": "${b}${c}", "b": "[x, y]", "c": "[Draft]"} {
		if fm[key] != want {
			t.Errorf("key %q: got %q, want %q", key, fm[key], want)
		}
	}
}

func TestGoTemplateConditionalWithFM(t *testing.T) {
	tp := newTemplateProcessor(nil, map[string]string{
		"Greeting": "Hello",
	})

	// Key present → truthy
	result, err := tp.processBody("test", ".", `{{ if .Greeting }}yes{{ else }}no{{ end }}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "yes" {
		t.Errorf("got %q, want %q", result, "yes")
	}

	// Key absent → falsy (zero value for string)
	result, err = tp.processBody("test", ".", `{{ if .Missing }}yes{{ else }}no{{ end }}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "no" {
		t.Errorf("got %q, want %q", result, "no")
	}
}

func TestNumberVerses(t *testing.T) {
	input := "First verse.\n\nSecond verse.\n\nThird verse."
	result := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)

	// Should be one continuous line (no double newlines)
	if strings.Contains(result, "\n\n") {
		t.Error("result should not contain paragraph breaks (double newlines)")
	}

	// First verse should have сⷯ prefix with space before numeral
	if !strings.Contains(result, "&{margin "+stixPrefix+" ") {
		t.Error("first verse should have стихъ prefix followed by space")
	}

	// Should contain verse markers
	if !strings.Contains(result, "&{margin ") || !strings.Contains(result, "}") {
		t.Error("result should contain margin note markers")
	}

	// Should have 3 markers
	count := strings.Count(result, "&{margin ")
	if count != 3 {
		t.Errorf("expected 3 margin markers, got %d", count)
	}
}

func TestNumberVersesSingleVerse(t *testing.T) {
	input := "Only verse."
	result := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)

	if !strings.Contains(result, "&{margin ") {
		t.Error("single verse should still get a margin marker")
	}
}

func TestVerseRange(t *testing.T) {
	// 5 verses numbered, then extract range 2–4
	input := "V1.\n\nV2.\n\nV3.\n\nV4.\n\nV5."
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)

	result, err := verseRange(2, 4, numbered)
	if err != nil {
		t.Fatal(err)
	}

	// Should have exactly 3 markers (verses 2, 3, 4)
	count := strings.Count(result, "&{margin ")
	if count != 3 {
		t.Errorf("expected 3 markers, got %d; result: %q", count, result)
	}

	// Should NOT contain the сⷯ prefix (that's on verse 1 only)
	if strings.Contains(result, stixPrefix) {
		t.Error("range 2-4 should not contain the stix prefix from verse 1")
	}

	// Should start with &{margin (the verse 2 marker)
	if !strings.HasPrefix(result, "&{margin ") {
		t.Errorf("should start with verse marker, got: %q", result[:20])
	}
}

func TestVerseRangeFromStart(t *testing.T) {
	input := "V1.\n\nV2.\n\nV3."
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)

	result, err := verseRange(1, 2, numbered)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 markers
	count := strings.Count(result, "&{margin ")
	if count != 2 {
		t.Errorf("expected 2 markers, got %d", count)
	}

	// Should preserve the сⷯ prefix on verse 1
	if !strings.Contains(result, stixPrefix) {
		t.Error("range starting at verse 1 should preserve stix prefix")
	}
}

func TestVerseRangeToEnd(t *testing.T) {
	input := "V1.\n\nV2.\n\nV3.\n\nV4.\n\nV5."
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)

	result, err := verseRange(4, 5, numbered)
	if err != nil {
		t.Fatal(err)
	}

	// Should have exactly 2 markers
	count := strings.Count(result, "&{margin ")
	if count != 2 {
		t.Errorf("expected 2 markers, got %d; result: %q", count, result)
	}

	// Should contain the text of verse 5
	if !strings.Contains(result, "V5.") {
		t.Error("should contain content of verse 5")
	}
}

func TestVerseRangeWithFirstVerseAsHeading(t *testing.T) {
	// Chain: numberVerses | verseRange 1 3 | firstVerseAsHeading "dropcap"
	input := "Heading.\n\nBody2.\n\nBody3.\n\nBody4."
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)
	ranged, err := verseRange(1, 3, numbered)
	if err != nil {
		t.Fatal(err)
	}
	result, err := firstVerseAsHeading("dropcap", ranged)
	if err != nil {
		t.Fatal(err)
	}

	// Should have heading + body separated by blank line
	parts := strings.SplitN(result, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected heading + body separated by blank line, got: %q", result)
	}

	heading := parts[0]
	if !strings.Contains(heading, "|=") || !strings.HasSuffix(heading, "=") {
		t.Errorf("heading should contain |=...= rubric, got: %q", heading)
	}

	body := parts[1]
	if !strings.Contains(body, "^") {
		t.Error("body should contain ^ for dropcap")
	}

	// Body should have 2 verse markers (verses 2 and 3)
	bodyMarkers := strings.Count(body, "&{margin ")
	if bodyMarkers != 2 {
		t.Errorf("body should have 2 verse markers, got %d", bodyMarkers)
	}
}

func TestFirstVerseAsHeading(t *testing.T) {
	// Simulate what numberVerses produces, then pipe through firstVerseAsHeading
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, "Heading verse.\n\nSecond verse.\n\nThird verse.")

	result, err := firstVerseAsHeading(numbered)
	if err != nil {
		t.Fatal(err)
	}

	// Margin marker first, then |= prefix
	if !strings.HasPrefix(result, "&{margin ") {
		t.Errorf("should start with &{margin, got: %q", result[:30])
	}
	if !strings.Contains(result, "|=") {
		t.Errorf("should contain |= heading prefix, got: %q", result[:40])
	}

	// Heading should end with = before the blank line
	parts := strings.SplitN(result, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatal("expected heading + body separated by blank line")
	}

	heading := parts[0]
	if !strings.HasSuffix(heading, "=") {
		t.Errorf("heading should end with =, got: %q", heading)
	}

	body := parts[1]
	// Without "dropcap" flag, body should NOT start with ^
	if strings.HasPrefix(body, "^") {
		t.Error("body should not have ^ prefix without dropcap flag")
	}

	// Body should have 2 markers (verse 2 and 3)
	count := strings.Count(body, "&{margin ")
	if count != 2 {
		t.Errorf("expected 2 margin markers in body, got %d", count)
	}
}

func TestFirstVerseAsHeadingDropcap(t *testing.T) {
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, "Heading verse.\n\nSecond verse.\n\nThird verse.")

	result, err := firstVerseAsHeading("dropcap", numbered)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.SplitN(result, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatal("expected heading + body separated by blank line")
	}

	body := parts[1]
	// With "dropcap" flag, body has &{margin marker then ^
	if !strings.Contains(body, "^") {
		t.Errorf("body should contain ^ when dropcap flag is set, got: %q", body[:30])
	}
}

func TestFirstVerseAsHeadingMultiVerse(t *testing.T) {
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, "First heading.\n\nSecond heading.\n\nThird verse.\n\nFourth verse.")

	// Request 2 verses as heading
	result, err := firstVerseAsHeading(2, "dropcap", numbered)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.SplitN(result, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatal("expected heading + body separated by blank line")
	}

	heading := parts[0]
	// Heading should contain both verse 1 and 2 markers
	markerCount := strings.Count(heading, "&{margin ")
	if markerCount != 2 {
		t.Errorf("heading should contain 2 verse markers, got %d; heading: %q", markerCount, heading)
	}

	body := parts[1]
	// Body should have ^ after any leading &{...} markers (dropcap)
	if !strings.Contains(body, "^") {
		t.Error("body should contain ^ when dropcap flag is set")
	}
	// Body should have 2 remaining verse markers (verse 3 and 4)
	bodyMarkers := strings.Count(body, "&{margin ")
	if bodyMarkers != 2 {
		t.Errorf("body should have 2 verse markers, got %d", bodyMarkers)
	}
}

func TestFirstVerseAsHeadingThenVerseRange(t *testing.T) {
	// Pipeline: numberVerses | firstVerseAsHeading "dropcap" | verseRange 1 3
	// verseRange slices at &{margin positions, so only the single-byte | prefix
	// before the first marker is lost — the =rubric= delimiters and ^ are preserved.
	input := "V1.\n\nV2.\n\nV3.\n\nV4.\n\nV5."
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, input)
	withHeading, err := firstVerseAsHeading("dropcap", numbered)
	if err != nil {
		t.Fatal(err)
	}
	result, err := verseRange(1, 3, withHeading)
	if err != nil {
		t.Fatal(err)
	}

	// Both |= and =...= should be preserved: verseRange slices at &{margin
	// which comes before |=, so the entire heading structure is included.
	if !strings.Contains(result, "|=") {
		t.Errorf("verseRange should preserve |= heading prefix, got: %q", result)
	}

	// Should have exactly 3 margin markers (verses 1, 2, 3)
	count := strings.Count(result, "&{margin ")
	if count != 3 {
		t.Errorf("expected 3 margin markers, got %d; result: %q", count, result)
	}

	// Body should contain ^ for dropcap (after &{margin marker)
	parts := strings.SplitN(result, "\n\n", 2)
	if len(parts) == 2 && !strings.Contains(parts[1], "^") {
		t.Errorf("body should contain ^ for dropcap, got: %q", parts[1])
	}
}

func TestSplitLeadingDirectives(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantDirectives string
		wantRest       string
	}{
		{"no directives", "hello", "", "hello"},
		{"single margin", "&{margin а҃}text", "&{margin а҃}", "text"},
		{"anchor then text", "&{anchor c1}heading", "&{anchor c1}", "heading"},
		{"two directives", "&{margin а҃}&{anchor c1}text", "&{margin а҃}&{anchor c1}", "text"},
		{"unclosed brace", "&{broken", "", "&{broken"},
		{"empty input", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotD, gotR := splitLeadingDirectives(tt.input)
			if gotD != tt.wantDirectives {
				t.Errorf("directives: got %q, want %q", gotD, tt.wantDirectives)
			}
			if gotR != tt.wantRest {
				t.Errorf("rest: got %q, want %q", gotR, tt.wantRest)
			}
		})
	}
}

func TestParagraphs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"two paragraphs", "A\n\nB", []string{"A", "B"}},
		{"internal line breaks kept", "A1\nA2\n\nB", []string{"A1\nA2", "B"}},
		{"several blank lines", "A\n\n\n\nB", []string{"A", "B"}},
		{"whitespace-only line is blank", "A\n  \nB", []string{"A", "B"}},
		{"edges trimmed", "\n\n  A  \n\nB\n", []string{"A", "B"}},
		{"crlf", "A\r\n\r\nB", []string{"A", "B"}},
		{"empty", "", nil},
		{"only whitespace", " \n\n\t\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paragraphs(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if cap(got) != len(got) {
				t.Errorf("cap %d != len %d: slice builtin would read past the last paragraph", cap(got), len(got))
			}
		})
	}
}

func TestParagraphsInTemplate(t *testing.T) {
	tp := newTemplateProcessor(nil, map[string]string{"Refrain": "-~R."})
	body := `{{ $t := paragraphs "~A\n\n~B\n\n~C\n" }}
{{ range slice $t 0 2 }}
{{ $.Refrain }}

-{{ . }}
{{ end }}

-{{ index $t 2 }}

{{ len $t }}`
	result, err := tp.processBody("test", ".", body)
	if err != nil {
		t.Fatal(err)
	}
	// Iteration seams must be paragraph breaks: normalise with the splitter.
	want := []string{"-~R.", "-~A", "-~R.", "-~B", "-~C", "3"}
	if got := paragraphs(result); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	// Out-of-range selections must fail loudly, including slice ends past len
	// (the slice builtin bounds-checks against cap, which paragraphs clamps to len).
	for _, bad := range []string{`{{ index $t 3 }}`, `{{ slice $t 0 4 }}`} {
		_, err := tp.processBody("test", ".", `{{ $t := paragraphs "A\n\nB\n\nC" }}`+bad)
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("%s: want out-of-range error, got %v", bad, err)
		}
	}
}

func TestVerseRangeErrors(t *testing.T) {
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, "V1.\n\nV2.\n\nV3.\n\nV4.\n\nV5.")
	tests := []struct {
		name       string
		start, end int
		text       string
	}{
		{"start below 1", 0, 2, numbered},
		{"end before start", 3, 2, numbered},
		{"end past last verse", 2, 9, numbered},
		{"start past last verse", 6, 6, numbered},
		{"no markers", 1, 1, "plain text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verseRange(tt.start, tt.end, tt.text)
			if err == nil || !strings.HasPrefix(err.Error(), "verseRange:") {
				t.Errorf("want verseRange error, got %v", err)
			}
		})
	}
}

func TestFirstVerseAsHeadingErrors(t *testing.T) {
	numbered := numberVerses(cu.ToCyrillicNumeral, stixPrefix, "Heading.\n\nBody.")
	tests := []struct {
		name string
		args []any
	}{
		{"no arguments", nil},
		{"unknown option", []any{"bold", numbered}},
		{"zero count", []any{0, numbered}},
		{"float count", []any{2.5, numbered}},
		{"text not a string", []any{"dropcap", 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := firstVerseAsHeading(tt.args...)
			if err == nil || !strings.HasPrefix(err.Error(), "firstVerseAsHeading:") {
				t.Errorf("want firstVerseAsHeading error, got %v", err)
			}
		})
	}
}

func TestTypedArgsInTemplate(t *testing.T) {
	tp := newTemplateProcessor(nil, map[string]string{})
	const verses = `"V1.\n\nV2.\n\nV3."`

	result, err := tp.processBody("test", ".", `{{ `+verses+` | numberVerses | verseRange 2 3 }}`)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(result, "&{margin "); n != 2 || strings.Contains(result, stixPrefix) {
		t.Errorf("verseRange 2 3: want verses 2-3 without the stix prefix, got %q", result)
	}

	result, err = tp.processBody("test", ".", `{{ `+verses+` | numberVerses | firstVerseAsHeading 2 "dropcap" }}`)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(result, "\n\n", 2)
	if len(parts) != 2 || strings.Count(parts[0], "&{margin ") != 2 || !strings.Contains(parts[1], "^") {
		t.Errorf("firstVerseAsHeading 2 \"dropcap\": want two-verse heading and ^ body, got %q", result)
	}

	// The numbers are integers: a quoted one is an error.
	_, err = tp.processBody("test", ".", `{{ `+verses+` | numberVerses | verseRange "2" "3" }}`)
	if err == nil || !strings.Contains(err.Error(), "expected integer") {
		t.Errorf("quoted numbers: want \"expected integer\" error, got %v", err)
	}
}

func TestFrontMatterKeepsLineNumbers(t *testing.T) {
	tp := newTemplateProcessor(nil, map[string]string{})

	// Front matter on lines 1-3, text on line 4, a bad directive on line 5:
	// the error must name line 5 of the file, not line 2 of the body.
	body, _ := stripFrontMatter("---\nDocumentTitle: x\n---\nline four\n{{ index $x 1 }}\n")
	_, err := tp.processBody("f.md", ".", body)
	if err == nil || !strings.Contains(err.Error(), "f.md:5:") {
		t.Errorf("want an error at f.md:5, got %v", err)
	}

	// The comment that keeps the lines emits nothing.
	body, _ = stripFrontMatter("---\nDocumentTitle: x\n---\nhello\n")
	out, err := tp.processBody("f.md", ".", body)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello\n" {
		t.Errorf("got %q, want %q", out, "hello\n")
	}

	// No front matter: nothing is added.
	body, _ = stripFrontMatter("hello\n")
	if body != "hello\n" {
		t.Errorf("got %q, want %q", body, "hello\n")
	}
}

// A function given by WithFunc is called as the package's own are.
func TestWithFunc(t *testing.T) {
	fsys := fstest.MapFS{
		"f.md": {Data: []byte(`{{ include "g.md" | shout }}`)},
		"g.md": {Data: []byte("amen")},
	}
	got, _, err := ProcessTemplate(fsys, "f.md", WithFunc("shout", strings.ToUpper))
	if err != nil {
		t.Fatal(err)
	}
	if want := "AMEN"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A document in several languages keeps a tree for each and includes by
// Lang, a key the template sees like any other: cu when not set, en by
// WithData (-set Lang=en).
func TestIncludeByLang(t *testing.T) {
	fsys := fstest.MapFS{
		"doc/s.md": {Data: []byte(`{{ include (print "../" .Lang "/a.md") }}`)},
		"cu/a.md":  {Data: []byte("сла́ва")},
		"en/a.md":  {Data: []byte("Glory")},
	}
	for _, tt := range []struct {
		set  map[string]string
		want string
	}{
		{nil, "сла́ва"},
		{map[string]string{"Lang": "en"}, "Glory"},
	} {
		got, _, err := ProcessTemplate(fsys, "doc/s.md", WithData(tt.set))
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%v: got %q, want %q", tt.set, got, tt.want)
		}
	}
}

// An include path is relative to the including file's directory and may
// climb with "..", in a nested include too; above the tree's root it is
// refused.
func TestIncludeTree(t *testing.T) {
	fsys := fstest.MapFS{
		"a/b/s.md":   {Data: []byte(`{{ include "../x.md" }}|{{ include "../../c/y.md" }}`)},
		"a/x.md":     {Data: []byte(`x {{ include "../c/y.md" }}`)},
		"c/y.md":     {Data: []byte("y")},
		"a/b/out.md": {Data: []byte(`{{ include "../../../c/y.md" }}`)},
	}
	got, _, err := ProcessTemplate(fsys, "a/b/s.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := "x y|y"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	_, _, err = ProcessTemplate(fsys, "a/b/out.md")
	if err == nil || !strings.Contains(err.Error(), `include "../../../c/y.md": outside the tree`) {
		t.Errorf("want an error refusing the path, got %v", err)
	}
}

// WithData sets keys over the front matter (the command's -set) before the
// template runs; the front matter returned carries them.
func TestWithData(t *testing.T) {
	fsys := fstest.MapFS{"f.md": {Data: []byte("---\nLang: cu\nDocumentTitle: x\n---\n{{ .Lang }} {{ .DocumentTitle }}")}}
	out, fm, err := ProcessTemplate(fsys, "f.md", WithData(map[string]string{"Lang": "en"}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "en x" || fm["Lang"] != "en" || fm["DocumentTitle"] != "x" {
		t.Errorf("got %q and %v", out, fm)
	}
}

// The front matter returned says the document's language also when the
// template does not: a caller (the page's lang) need not know the default.
func TestLangDefault(t *testing.T) {
	fsys := fstest.MapFS{"f.md": {Data: []byte("---\nDocumentTitle: x\n---\ntext")}}
	_, fm, err := ProcessTemplate(fsys, "f.md")
	if err != nil {
		t.Fatal(err)
	}
	if fm["Lang"] != "cu" {
		t.Errorf("got Lang %q, want cu", fm["Lang"])
	}
}

// A marked number is written in the document's numerals whichever way the
// text writes it, inside a margin note and a page context value too.
func TestExpandNum(t *testing.T) {
	in := "зача́ло &{num сл҃г}, &{num 233}. &{margin На &{num і҃}:}x &{push psalms &{num  слг }}"
	for _, tt := range []struct {
		write func(int) string
		want  string
	}{
		{cu.ToCyrillicNumeral, "зача́ло сл҃г, сл҃г. &{margin На і҃:}x &{push psalms сл҃г}"},
		{strconv.Itoa, "зача́ло 233, 233. &{margin На 10:}x &{push psalms 233}"},
	} {
		got, err := expandNum(tt.write, in)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
	if _, err := expandNum(strconv.Itoa, "гла́съ &{num слб}"); err == nil || !strings.Contains(err.Error(), "&{num слб}") {
		t.Errorf("a marker that is not a number: got %v, want an error quoting it", err)
	}
	// An escaped & makes the marker text, a number or not; an escaped
	// backslash before it does not.
	in = `\&{num 5} \&{num слб} \\&{num 5}`
	want := `\&{num 5} \&{num слб} \\5`
	if got, err := expandNum(strconv.Itoa, in); err != nil || got != want {
		t.Errorf("escaped markers: got %q, %v, want %q", got, err, want)
	}
}

// Numerals defaults from Lang, is returned in the front matter, and governs
// the markers, those a template makes of a number it computed (printf) and
// those written in the text, and numberVerses; a value other than
// cyrillic or arabic is an error naming the key. numberVerses writes «сⷯ»
// before the first verse's number by Lang alone: in cu with either numerals,
// in en with neither.
func TestNumerals(t *testing.T) {
	fsys := fstest.MapFS{"f.md": {Data: []byte("{{ printf \"&{num %d}\" 26 }} &{num к҃ѕ} {{ \"A\\n\\nB\" | numberVerses }}")}}
	for _, tt := range []struct {
		set      map[string]string
		numerals string
		want     string
	}{
		{nil, "cyrillic", "к҃ѕ к҃ѕ &{margin " + stixPrefix + " а҃}A &{margin в҃}B"},
		{map[string]string{"Lang": "en"}, "arabic", "26 26 &{margin 1}A &{margin 2}B"},
		{map[string]string{"Numerals": "arabic"}, "arabic", "26 26 &{margin " + stixPrefix + " 1}A &{margin 2}B"},
		{map[string]string{"Lang": "en", "Numerals": "cyrillic"}, "cyrillic", "к҃ѕ к҃ѕ &{margin а҃}A &{margin в҃}B"},
	} {
		out, fm, err := ProcessTemplate(fsys, "f.md", WithData(tt.set))
		if err != nil {
			t.Fatal(err)
		}
		if out != tt.want || fm["Numerals"] != tt.numerals {
			t.Errorf("%v: got %q and Numerals %q, want %q and %q", tt.set, out, fm["Numerals"], tt.want, tt.numerals)
		}
	}
	_, _, err := ProcessTemplate(fsys, "f.md", WithData(map[string]string{"Numerals": "roman"}))
	if err == nil || !strings.Contains(err.Error(), "Numerals") {
		t.Errorf("Numerals=roman: got %v, want an error naming the key", err)
	}
}

// A key that is not set stops the render, whether printed bare («<no
// value>») or through printf («%!s(<nil>)»), quoting the line.
func TestHoles(t *testing.T) {
	for _, body := range []string{
		"a\n\nb {{ .Missing }}",
		"a\n\nb {{ printf \"гла́съ %s\" .Missing }}",
	} {
		_, _, err := ProcessTemplate(fstest.MapFS{"f.md": {Data: []byte(body)}}, "f.md")
		if err == nil || !strings.Contains(err.Error(), "the first: b ") {
			t.Errorf("%q: got %v, want an error quoting the line", body, err)
		}
	}
	fsys := fstest.MapFS{"f.md": {Data: []byte("---\nX: x\n---\n{{ .X }} {{ printf \"%s\" .X }}")}}
	if _, _, err := ProcessTemplate(fsys, "f.md"); err != nil {
		t.Errorf("every key set: %v", err)
	}
	// A «%!» of the text is no hole.
	fsys = fstest.MapFS{"f.md": {Data: []byte("A sale: 100%! off today.")}}
	if _, _, err := ProcessTemplate(fsys, "f.md"); err != nil {
		t.Errorf("a %%! in the text: %v", err)
	}
	// fmt's other complaints are holes too.
	fsys = fstest.MapFS{"f.md": {Data: []byte(`{{ printf "%d" "x" }} {{ printf "a" 1 }}`)}}
	if _, _, err := ProcessTemplate(fsys, "f.md"); err == nil {
		t.Error("a wrong verb and an extra argument: no error, want one")
	}
}
