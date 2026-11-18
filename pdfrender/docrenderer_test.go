package pdfrender

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/grisha/slavonic-render/internal/expreval"
	"github.com/grisha/slavonic-render/ir"
)

// An expression of the body sees a key of the front matter as its text. One
// that fails is an error naming the expression and the evaluator's reason;
// only an anchor not yet placed defers its paragraph (ErrNeedsDefer).
func TestResolveSpans(t *testing.T) {
	dr := &DocumentRenderer{
		anchorPages: map[string]int{},
		exprVars:    map[string]string{"Feast": "Pascha"},
	}
	dr.exprFuncs = dr.buildExprFuncs()
	resolve := func(expr string) (string, error) {
		spans, err := dr.resolveSpans([]ir.Span{{Expr: expr}})
		if err != nil {
			return "", err
		}
		return spans[0].Text, nil
	}

	if got, err := resolve(`upper(Feast)`); err != nil || got != "PASCHA" {
		t.Errorf("upper(Feast) = %q, %v; want PASCHA", got, err)
	}

	for _, tt := range []struct {
		expr   string
		reason string // what the error says besides the expression
	}{
		{`nosuch`, "unknown name nosuch"},
		{`1 +`, "unexpected token"},
		{`first(Feast)`, "cannot get first element from string"},
	} {
		_, err := resolve(tt.expr)
		if err == nil {
			t.Errorf("%s: no error", tt.expr)
			continue
		}
		if errors.Is(err, expreval.ErrNeedsDefer) {
			t.Errorf("%s: a mistake must not be deferred: %v", tt.expr, err)
		}
		for _, want := range []string{"${" + tt.expr + "}", tt.reason} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not say %q", tt.expr, err, want)
			}
		}
	}

	_, err := resolve(`page("c9")`)
	if !errors.Is(err, expreval.ErrNeedsDefer) {
		t.Errorf(`page("c9"): want an error wrapping ErrNeedsDefer, got %v`, err)
	} else if want := `${page("c9")}`; !strings.Contains(err.Error(), want) {
		t.Errorf(`page("c9"): error %q does not say %q`, err, want)
	}
}

// A footnote's expressions are evaluated as a paragraph's are: one that
// fails is an error naming the footnote and the expression, not a footnote
// with a hole in it. A footnote cannot be deferred, so an anchor not yet
// placed is such an error too.
func TestFootnoteExpressions(t *testing.T) {
	for _, expr := range []string{`nosuch`, `page("c9")`} {
		dr := &DocumentRenderer{
			anchorPages:   map[string]int{},
			exprVars:      map[string]string{"Feast": "Pascha"},
			footnoteByNum: map[int]ir.FootnoteDef{1: {Num: 1, Content: []ir.Span{ir.T("A note on "), {Expr: expr}}}},
		}
		dr.exprFuncs = dr.buildExprFuncs()
		_, err := dr.preRenderFootnotes([]ir.Span{ir.T("Text."), {FootnoteRef: 1}})
		if err == nil {
			t.Errorf("%s: no error", expr)
			continue
		}
		for _, want := range []string{"footnote 1", "${" + expr + "}"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not say %q", expr, err, want)
			}
		}
	}
}

// newTestDocument makes a DocumentRenderer as the command makes one, its
// PDF in the test's temporary directory.
func newTestDocument(t *testing.T) *DocumentRenderer {
	t.Helper()
	dr, err := NewDocumentFromConfig(DocumentConfig{
		OutputPath:      filepath.Join(t.TempDir(), "test.pdf"),
		BodyFontName:    "Ponomar",
		DropCapFontName: "Indiction",
		Config:          Config{BodySize: bag.MustSP("14pt"), DropCap: DropCapConfig{Lines: 2, Overhang: 1.2}},
		PageConfig:      PageConfig{Margin: bag.MustSP("15mm"), Numeral: strconv.Itoa},
	})
	if err != nil {
		t.Fatal(err)
	}
	return dr
}

// A paragraph that waits for a page number (it names an anchor not yet
// placed) keeps its footnote: the footnote's text is whole, so it goes to
// the foot of the paragraph's page at once, before the footnote of the
// paragraph after it.
func TestDeferredFootnote(t *testing.T) {
	doc := &ir.Document{
		Blocks: []ir.Block{
			{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("See page "), {Expr: `page("later")`}, ir.T("."), {FootnoteRef: 1}}},
			{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("Plain paragraph."), {FootnoteRef: 2}}},
		},
		Footnotes: []ir.FootnoteDef{
			{ID: "a", Num: 1, Content: []ir.Span{ir.T("The first note.")}},
			{ID: "b", Num: 2, Content: []ir.Span{ir.T("The second note.")}},
		},
	}
	dr := newTestDocument(t)
	if err := dr.Render(doc); err != nil {
		t.Fatal(err)
	}
	if !dr.HasDeferred() {
		t.Fatal("the first paragraph did not wait for its page number")
	}
	var got []string
	for _, vl := range dr.pageBuilder.pageFootnotes {
		got = append(got, extractFirstWordText(vl))
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "1.") || !strings.HasPrefix(got[1], "2.") {
		t.Errorf("the page's footnotes begin %q, want note 1 and then note 2", got)
	}
}

// A centred line that waits for a page number is set as a centred line:
// it takes the room of one that does not wait, the rubric's space above and
// below included, and the paragraph it is set as is centred.
func TestDeferredCentred(t *testing.T) {
	afterCentred := func(content []ir.Span) bag.ScaledPoint {
		t.Helper()
		dr := newTestDocument(t)
		err := dr.Render(&ir.Document{Blocks: []ir.Block{
			{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("A plain paragraph first.")}},
			{Kind: ir.BlockCentered, Content: content},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return dr.pageBuilder.currentY
	}
	waits := afterCentred([]ir.Span{ir.T("See page "), {Expr: `page("later")`}, ir.T(".")})
	setNow := afterCentred([]ir.Span{ir.T("See page 2.")})
	if waits != setNow {
		t.Errorf("after a centred line that waits the page is at %v, after one that does not at %v", waits, setNow)
	}

	if !deferredParagraph(ir.Block{Kind: ir.BlockCentered}, nil).Centered {
		t.Error("a centred block that waits is not set centred")
	}
	if p := deferredParagraph(ir.Block{Kind: ir.BlockParagraph, Indented: true, Minor: true}, nil); p.Centered || !p.Indented || !p.Minor {
		t.Errorf("an indented minor paragraph that waits is set as %+v", p)
	}
}

// A drop cap paragraph and a paragraph with a margin note cannot wait for a
// page number: each is an error that says which and names the expression,
// not a paragraph set without its initial or its note.
func TestDeferredRefused(t *testing.T) {
	waiting := []ir.Span{ir.T("See page "), {Expr: `page("later")`}, ir.T(", and more words.")}
	for _, tc := range []struct {
		what  string
		block ir.Block
	}{
		{"drop cap", ir.Block{Kind: ir.BlockDropCap, Content: waiting}},
		{"margin note", ir.Block{Kind: ir.BlockParagraph, Content: waiting, Notes: []ir.MarginNote{{CharPos: 0, Text: "N1"}}}},
	} {
		err := newTestDocument(t).Render(&ir.Document{Blocks: []ir.Block{tc.block}})
		if err == nil {
			t.Errorf("%s: no error", tc.what)
			continue
		}
		for _, want := range []string{tc.what, `${page("later")}`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not say %q", tc.what, err, want)
			}
		}
	}
}
