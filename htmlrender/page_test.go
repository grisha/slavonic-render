package htmlrender

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/grisha/slavonic-render/fonts"
	"github.com/grisha/slavonic-render/ir"
)

// The page is the document's head, the font, the style sheet, the caller's
// part of the head as it is written, and the document. The example menu
// is that part here, so that the example is known to be where the README
// says it is.
func TestRenderPage(t *testing.T) {
	doc := &ir.Document{Blocks: []ir.Block{
		{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("Поми́лꙋй мѧ̀, бж҃е")}},
	}}
	menu, err := os.ReadFile("../examples/menu.html")
	if err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	if err := RenderPage(&page, doc, nil, "Psalm <50>", "cu", "", string(menu)); err != nil {
		t.Fatal(err)
	}
	got := page.String()

	if !strings.HasPrefix(got, "<!DOCTYPE html>\n<html lang=\"cu\">\n") {
		t.Errorf("the page begins %q", got[:40])
	}
	if !strings.HasSuffix(got, "</article>\n</body>\n</html>\n") {
		t.Errorf("the page ends %q", got[len(got)-100:])
	}
	for _, part := range []string{
		"<title>Psalm &lt;50&gt;</title>",
		"@font-face {\n\tfont-family: \"" + defaultBodyFont + "\";\n",
		// "T1RUT" is how base64 begins "OTTO", which opens an OpenType
		// font with CFF outlines.
		`src: url("data:font/otf;base64,T1RUT`,
		"@media screen {\n\t:root {\n\t\tcolor-scheme: light dark;",
		":root[data-theme=\"dark\"] {\n\t\tcolor-scheme: dark;",
		// The caller's part ends the head, after the style sheet, and
		// the document is all there is in the body.
		CSS + "</style>\n" + string(menu) + "</head>\n<body>\n<article class=\"slavonic\">\n<p>Поми́лꙋй мѧ̀, бж҃е</p>\n</article>\n</body>\n",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("the page lacks %.60q", part)
		}
	}
}

func TestRenderPageWriteError(t *testing.T) {
	err := RenderPage(failingWriter{}, &ir.Document{}, nil, "", "cu", "", "")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("got %v, want the writer's error", err)
	}
}

// A page set in another font has that font inside it, under its own name,
// and a rule after the style sheet that gives the name to --body; a font
// that is not embedded is an error naming it.
func TestRenderPageBodyFont(t *testing.T) {
	data, err := fonts.ReadData("Triodion")
	if err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	if err := RenderPage(&page, &ir.Document{}, nil, "", "cu", "Triodion", ""); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"@font-face {\n\tfont-family: \"Triodion\";\n\tsrc: url(\"data:font/otf;base64," + base64.StdEncoding.EncodeToString(data) + "\")",
		CSS + ".slavonic {\n\t--body: \"Triodion\", serif;\n}\n</style>\n",
	} {
		if !strings.Contains(page.String(), part) {
			t.Errorf("the page lacks %.60q", part)
		}
	}

	err = RenderPage(&page, &ir.Document{}, nil, "", "cu", "Nosuch", "")
	if err == nil || !strings.Contains(err.Error(), "Nosuch") {
		t.Errorf("got %v, want an error naming the font", err)
	}
}

// The font a page has inside it when none is asked for is the one the
// style sheet names for the text, and no rule names a face but through a
// property, so that a page that sets --body changes the face everywhere.
func TestCSSBodyFont(t *testing.T) {
	if decl := "--body: \"" + defaultBodyFont + "\", serif;"; strings.Count(CSS, decl) != 1 {
		t.Errorf("the style sheet lacks %q (defaultBodyFont)", decl)
	}
	if strings.Contains(CSS, "font-family: \"") {
		t.Errorf("a rule of the style sheet names a face itself, not by var(--body) or var(--plain)")
	}
}
