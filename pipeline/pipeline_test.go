package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grisha/slavonic-render/markup"
	"github.com/grisha/slavonic-render/pdfrender"
)

// The document's title is its first # heading, as the heading's text is
// set (its markup gone), and the front matter's DocumentTitle in its place
// when there is one; a document with neither has no title.
func TestTitle(t *testing.T) {
	for _, tt := range []struct {
		markdown string
		fm       map[string]string
		want     string
	}{
		{"text\n\n## Section\n\n# Psalm =22=\n\n# Psalm 23\n", nil, "Psalm 22"},
		{"# Psalm 22\n", map[string]string{"DocumentTitle": "The Psalter"}, "The Psalter"},
		{"## Section\n\ntext\n", nil, ""},
	} {
		doc, err := markup.Parse(tt.markdown)
		if err != nil {
			t.Fatal(err)
		}
		if got := Title(tt.fm, doc); got != tt.want {
			t.Errorf("%q with %v: the title is %q, want %q", tt.markdown, tt.fm, got, tt.want)
		}
	}
}

// The page numbers are written in PageNumerals, the document's Numerals
// when it is not set; a value other than cyrillic or arabic is an error
// naming the key.
func TestPageNumerals(t *testing.T) {
	for _, tt := range []struct {
		fm   map[string]string
		want string
	}{
		{map[string]string{"Numerals": "cyrillic"}, "к҃ѕ"},
		{map[string]string{"Numerals": "arabic"}, "26"},
		{map[string]string{"Numerals": "cyrillic", "PageNumerals": "arabic"}, "26"},
		{map[string]string{"Numerals": "arabic", "PageNumerals": "cyrillic"}, "к҃ѕ"},
	} {
		var pdf pdfrender.DocumentConfig
		if err := applyFrontMatter(tt.fm, &pdf); err != nil {
			t.Fatal(err)
		}
		if got := pdf.Numeral(26); got != tt.want {
			t.Errorf("%v: page 26 is %q, want %q", tt.fm, got, tt.want)
		}
	}
	err := applyFrontMatter(map[string]string{"Numerals": "cyrillic", "PageNumerals": "roman"}, &pdfrender.DocumentConfig{})
	if err == nil || !strings.Contains(err.Error(), "PageNumerals") {
		t.Errorf("PageNumerals=roman: got %v, want an error naming the key", err)
	}
}

// The fonts and the drop cap's proportions are the front matter's: a key
// that is set replaces what the configuration held, a key that is not
// leaves it, and a number that cannot be read is an error naming the key.
func TestFontAndDropCapKeys(t *testing.T) {
	var r pdfrender.DocumentConfig
	r.BodyFontName = "Ponomar"
	r.DropCapFontName = "Indiction"
	r.DropCap.Lines = 2
	r.DropCap.Overhang = 1.2
	fm := map[string]string{"Numerals": "cyrillic", "DropCapFont": "Vertograd", "DropCapLines": "3", "DropCapOverhang": "1.1"}
	if err := applyFrontMatter(fm, &r); err != nil {
		t.Fatal(err)
	}
	if r.BodyFontName != "Ponomar" || r.DropCapFontName != "Vertograd" || r.DropCap.Lines != 3 || r.DropCap.Overhang != 1.1 {
		t.Errorf("got %s, %s, %d, %g; want Ponomar, Vertograd, 3, 1.1",
			r.BodyFontName, r.DropCapFontName, r.DropCap.Lines, r.DropCap.Overhang)
	}
	for key, value := range map[string]string{"DropCapLines": "two", "DropCapOverhang": "big"} {
		err := applyFrontMatter(map[string]string{"Numerals": "cyrillic", key: value}, &pdfrender.DocumentConfig{})
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%s: got %v, want an error naming the key", key, value, err)
		}
	}
}

// RunHTML writes the page into a file whose directory it makes, with the
// front matter's language, title and font, the caller's head and the
// dumps beside it; without an output file there is nothing to name a dump
// after, and asking for one is an error.
func TestRunHTML(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("---\nLang: en\nBodyFont: Triodion\n---\n# A title\n\nSome text.\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "new", "out.html")
	cfg := &Config{InputPath: in, OutputPath: out, DumpMD: true, DumpIR: true}
	if err := RunHTML(cfg, `<meta name="mine">`); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<html lang="en">`, `<title>A title</title>`, `--body: "Triodion", serif;`, `<meta name="mine">`, "Some text."} {
		if !strings.Contains(string(page), want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	for _, dump := range []string{out + ".debug.md", out + ".debug.json"} {
		if _, err := os.Stat(dump); err != nil {
			t.Errorf("the dump: %v", err)
		}
	}

	err = RunHTML(&Config{InputPath: in, DumpMD: true}, "")
	if err == nil || !strings.Contains(err.Error(), "output file") {
		t.Errorf("a dump without an output file: got %v, want an error naming the output file", err)
	}
}
