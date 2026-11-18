package markup

import (
	"strings"
	"testing"

	"github.com/grisha/slavonic-render/ir"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ---- Test helpers ----

// parse creates a goldmark parser with our extension and parses the input.
func parse(t *testing.T, input string) (ast.Node, []byte) {
	t.Helper()
	source := []byte(input)
	md := goldmark.New(goldmark.WithExtensions(&churchSlavonicExtension{}))
	doc := md.Parser().Parse(text.NewReader(source))
	return doc, source
}

// firstOfKind walks the AST depth-first and returns the first node matching kind.
func firstOfKind(root ast.Node, kind ast.NodeKind) ast.Node {
	var found ast.Node
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == kind {
			found = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return found
}

// allOfKind collects all nodes matching kind.
func allOfKind(root ast.Node, kind ast.NodeKind) []ast.Node {
	var nodes []ast.Node
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == kind {
			nodes = append(nodes, n)
		}
		return ast.WalkContinue, nil
	})
	return nodes
}

// nodeText extracts the text content of a node from source bytes.
func nodeText(n ast.Node, source []byte) string {
	var buf []byte
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf = append(buf, t.Segment.Value(source)...)
		} else {
			// Recurse for nested inlines (e.g. rubricInline inside rubricInline).
			buf = append(buf, nodeText(child, source)...)
		}
	}
	return string(buf)
}

// firstParagraph returns the first ast.Paragraph in the tree.
func firstParagraph(root ast.Node) *ast.Paragraph {
	n := firstOfKind(root, ast.KindParagraph)
	if n == nil {
		return nil
	}
	return n.(*ast.Paragraph)
}

// collectText walks the AST and concatenates all raw text segments.
func collectText(root ast.Node, source []byte) string {
	var buf []byte
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := n.(*ast.Text); ok {
			buf = append(buf, t.Segment.Value(source)...)
		}
		return ast.WalkContinue, nil
	})
	return string(buf)
}

// ---- Inline parser tests ----

func TestRubricInline(t *testing.T) {
	doc, source := parse(t, "=rubric text=")
	n := firstOfKind(doc, kindRubricInline)
	if n == nil {
		t.Fatal("expected rubricInline node, got nil")
	}
	got := nodeText(n, source)
	if got != "rubric text" {
		t.Errorf("rubricInline text = %q, want %q", got, "rubric text")
	}
}

func TestPlainTextInline(t *testing.T) {
	doc, source := parse(t, "*plain text*")
	n := firstOfKind(doc, kindPlainTextInline)
	if n == nil {
		t.Fatal("expected plainTextInline node, got nil")
	}
	got := nodeText(n, source)
	if got != "plain text" {
		t.Errorf("plainTextInline text = %q, want %q", got, "plain text")
	}
}

func TestIncipitASCII(t *testing.T) {
	doc, source := parse(t, `~Б`)
	n := firstOfKind(doc, kindRubricInline)
	if n == nil {
		t.Fatal("expected rubricInline (incipit) node, got nil")
	}
	got := nodeText(n, source)
	if got != "Б" {
		t.Errorf("incipit text = %q, want %q", got, "Б")
	}
}

func TestIncipitMultiByteGrapheme(t *testing.T) {
	// Church Slavonic letter with combining marks: base + titlo (U+0483)
	input := "~Б\u0483rest"
	doc, source := parse(t, input)
	n := firstOfKind(doc, kindRubricInline)
	if n == nil {
		t.Fatal("expected rubricInline (incipit) node, got nil")
	}
	got := nodeText(n, source)
	want := "Б\u0483"
	if got != want {
		t.Errorf("incipit text = %q, want %q", got, want)
	}
}

func TestIncipitAfterMarginDirective(t *testing.T) {
	// numberVerses output piped through ~ arrives as "~&{margin в҃}Гдⷭ҇и".
	// The directive must come out as its own node ahead of the incipit,
	// which colours the first grapheme after it.
	doc, source := parse(t, "~&{margin в҃}Гдⷭ҇и, что́")
	para := firstParagraph(doc)
	if para == nil {
		t.Fatal("expected a paragraph")
	}
	first := para.FirstChild()
	pc, ok := first.(*pageContextInline)
	if !ok {
		t.Fatalf("first child = %T, want *pageContextInline", first)
	}
	if pc.Op != "margin" || pc.Key != "в҃" {
		t.Errorf("got Op=%q Key=%q, want margin/в҃", pc.Op, pc.Key)
	}
	second := first.NextSibling()
	if second == nil || second.Kind() != kindRubricInline {
		t.Fatalf("second child = %v, want rubricInline", second)
	}
	if got := nodeText(second, source); got != "Г" {
		t.Errorf("incipit text = %q, want %q", got, "Г")
	}
}

func TestIndentMarker(t *testing.T) {
	doc, _ := parse(t, "-text after")
	n := firstOfKind(doc, kindIndentMarker)
	if n == nil {
		t.Fatal("expected indentMarker node, got nil")
	}
}

func TestExpressionInline(t *testing.T) {
	doc, _ := parse(t, "${expr}")
	n := firstOfKind(doc, kindExpressionInline)
	if n == nil {
		t.Fatal("expected expressionInline node, got nil")
	}
	ei := n.(*expressionInline)
	if ei.Expr != "expr" {
		t.Errorf("Expr = %q, want %q", ei.Expr, "expr")
	}
}

func TestExpressionWithParens(t *testing.T) {
	doc, _ := parse(t, "${page(c1)}")
	n := firstOfKind(doc, kindExpressionInline)
	if n == nil {
		t.Fatal("expected expressionInline node, got nil")
	}
	ei := n.(*expressionInline)
	if ei.Expr != "page(c1)" {
		t.Errorf("Expr = %q, want %q", ei.Expr, "page(c1)")
	}
}

func TestExpressionEmpty(t *testing.T) {
	doc, _ := parse(t, "${}")
	n := firstOfKind(doc, kindExpressionInline)
	if n != nil {
		t.Error("expected no expressionInline for empty ${}, got one")
	}
}

func TestDollarAlone(t *testing.T) {
	doc, _ := parse(t, "$ alone")
	n := firstOfKind(doc, kindExpressionInline)
	if n != nil {
		t.Error("expected no expressionInline for bare $, got one")
	}
}

func TestPageContextPushThreeParts(t *testing.T) {
	doc, _ := parse(t, "&{push psalms 17}")
	n := firstOfKind(doc, kindPageContextInline)
	if n == nil {
		t.Fatal("expected pageContextInline node, got nil")
	}
	pc := n.(*pageContextInline)
	if pc.Op != "push" || pc.Key != "psalms" || pc.Value != "17" {
		t.Errorf("got Op=%q Key=%q Value=%q, want push/psalms/17", pc.Op, pc.Key, pc.Value)
	}
}

// A push is a block of the page context, and the one directive that is.
func TestPageContextBlock(t *testing.T) {
	doc, err := Parse("&{push psalms 17}\n\nтекст\n")
	if err != nil {
		t.Fatal(err)
	}
	var got []ir.Block
	for _, b := range doc.Blocks {
		if b.Kind == ir.BlockPageContextOp {
			got = append(got, b)
		}
	}
	if len(got) != 1 || got[0].Key != "psalms" || got[0].Value != "17" {
		t.Errorf("page context blocks = %+v, want one, psalms/17", got)
	}
}

// What the walker has no block or span for is an error naming it, and is
// not dropped without a word: the mistyped directive, the text beside an
// ornament, the list.
func TestRefused(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"mistyped directive", "&{anhcor c1}\n\nтекст\n", "&{anhcor c1}: not a directive"},
		{"an operation there once was", "&{set tone 5}", "&{set tone 5}: not a directive"},
		{"directive in a heading", "## Глава &{anhcor c1}", "&{anhcor c1}"},
		{"directive in a footnote", "текст[^n]\n\n[^n]: &{anhcor c1} note", "&{anhcor c1}"},
		{"anchor of two names", "&{anchor a b}", "an anchor has one name"},
		{"leader of two glyphs", "а &{leader . .} б", "a leader has one glyph"},
		{"push without a value", "&{push psalms}", "a push has a key and a value"},
		{"text after an ornament", "![](panel)\nтекст", "![](panel): an ornament is a paragraph of its own"},
		{"text before an ornament", "текст ![](panel)", "![](panel)"},
		{"anchor on an ornament's line", "&{anchor a} ![](panel)", "![](panel)"},
		{"ornament in a heading", "# Глава ![](panel)", "![](panel)"},
		{"link out of the document", "[текст](https://example.com)", "[](https://example.com): a link goes to an anchor"},
		{"link without the #", "[текст](c1)", "[](c1): a link goes to an anchor"},
		{"autolink", "см. <https://example.com> тамъ", "a Markdown autolink is not supported: <https://example.com>"},
		{"inline HTML", "текст<br>текст", "inline HTML is not supported: <br>"},
		{"a word in angle brackets", "the text <sic> stands", "inline HTML is not supported: <sic>"},
		{"bullet list", "- текст", `a Markdown List is not supported: "текст"`},
		{"plus and a space", "+ текст", "a Markdown List"},
		{"numbered list", "1. текст", "a Markdown List"},
		{"list in a minor paragraph", "> + текст", "a Markdown List"},
		{"indented code", "    текст", "a Markdown CodeBlock"},
		{"fenced code", "```\nтекст\n```", `a Markdown FencedCodeBlock is not supported: "текст"`},
		{"HTML block", "<div>текст</div>", "a Markdown HTMLBlock"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.src)
			if err == nil {
				t.Fatalf("Parse(%q): no error, want one holding %q", tt.src, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse(%q) = %v, want it to hold %q", tt.src, err, tt.want)
			}
		})
	}
}

// A line of anchors and pushes above a paragraph is a paragraph of its
// own, as with a blank line under it: the paragraph keeps its prefix, and
// the directives leave no text behind. A margin note is text of its
// paragraph and stays in it.
func TestDirectiveLines(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"above a drop cap", "&{anchor c1}\n^Текст", "anchor dropcap"},
		{"with the blank line", "&{anchor c1}\n\n^Текст", "anchor dropcap"},
		{"two lines", "&{anchor c2}\n&{push psalms ѳ҃}\n^Текст\nдалѣе", "anchor page-context-op dropcap"},
		{"two on a line", "&{anchor c2} &{push psalms ѳ҃}\n|Текст", "anchor page-context-op centered"},
		{"indented by spaces", "  &{push psalms ѳ҃}  \n+Текст", "page-context-op paragraph"},
		{"above plain text", "&{push psalms ѳ҃}\nТекст", "page-context-op paragraph"},
		{"above an ornament", "&{anchor c1}\n![](panel)", "anchor image"},
		{"above a running header", "&{push psalms ѳ҃}\n%Каѳі́сма", "page-context-op running-header"},
		{"in a minor paragraph", "> &{push psalms ѳ҃}\n> ^Текст", "page-context-op dropcap"},
		{"alone", "&{anchor c2}\n&{push psalms ѳ҃}", "anchor page-context-op"},
		{"alone, on a line", "&{anchor c2} &{push psalms ѳ҃}", "anchor page-context-op"},
		{"a margin note is not one", "&{margin а҃}\nТекст", "paragraph"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, b := range doc.Blocks {
				kinds = append(kinds, b.Kind.String())
			}
			if got := strings.Join(kinds, " "); got != tt.want {
				t.Errorf("Parse(%q) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}

	// The paragraph under the line is whole: its text, nothing before it.
	doc, err := Parse("&{push psalms ѳ҃}\n^Текст\nдалѣе")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Blocks[1].Content; len(got) != 2 || got[0].Text != "Текст " || got[1].Text != "далѣе" {
		t.Errorf("spans = %+v, want «Текст », «далѣе»", got)
	}
}

// What stays: each directive with its parts, and an ornament alone in its
// paragraph, whatever stands between the brackets.
func TestNotRefused(t *testing.T) {
	doc, err := Parse("&{anchor c1}\n\n![орнамент](panel)\n\n> ![](line1)\n\n" +
		"&{push psalms 17}\n\n~&{margin сⷯ а҃}Текст &{leader .} к҃.\n")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, b := range doc.Blocks {
		kinds = append(kinds, b.Kind.String())
	}
	if got, want := strings.Join(kinds, " "), "anchor image image page-context-op paragraph"; got != want {
		t.Errorf("blocks = %s, want %s", got, want)
	}
	if doc.Blocks[1].Text != "panel" || !doc.Blocks[2].Minor {
		t.Errorf("ornaments = %+v, %+v, want panel, then a minor one", doc.Blocks[1], doc.Blocks[2])
	}
}

func TestPageContextMarginWithValue(t *testing.T) {
	doc, _ := parse(t, "&{margin сⷯ а҃}")
	n := firstOfKind(doc, kindPageContextInline)
	if n == nil {
		t.Fatal("expected pageContextInline node, got nil")
	}
	pc := n.(*pageContextInline)
	if pc.Op != "margin" || pc.Key != "сⷯ" || pc.Value != "а҃" {
		t.Errorf("got Op=%q Key=%q Value=%q, want margin/сⷯ/а҃", pc.Op, pc.Key, pc.Value)
	}
}

func TestDirectiveTwoParts(t *testing.T) {
	doc, _ := parse(t, "&{anchor c1}")
	n := firstOfKind(doc, kindPageContextInline)
	if n == nil {
		t.Fatal("expected pageContextInline node, got nil")
	}
	pc := n.(*pageContextInline)
	if pc.Op != "anchor" || pc.Key != "c1" || pc.Value != "" {
		t.Errorf("got Op=%q Key=%q Value=%q, want anchor/c1/\"\"", pc.Op, pc.Key, pc.Value)
	}
}

func TestPageContextEmpty(t *testing.T) {
	doc, _ := parse(t, "&{}")
	n := firstOfKind(doc, kindPageContextInline)
	if n != nil {
		t.Error("expected no pageContextInline for empty &{}, got one")
	}
}

func TestPageContextSingleWord(t *testing.T) {
	doc, _ := parse(t, "&{single}")
	n := firstOfKind(doc, kindPageContextInline)
	if n != nil {
		t.Error("expected no pageContextInline for single-word &{single}, got one")
	}
}

// ---- Paragraph transformer tests ----

func TestCenteredParagraph(t *testing.T) {
	doc, source := parse(t, "|centered text")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, ok := p.Attribute([]byte(attrCentered))
	if !ok {
		t.Error("expected centered attribute on paragraph")
	}
	// The | prefix should be stripped from content.
	got := nodeText(p, source)
	if got != "centered text" {
		t.Errorf("paragraph text = %q, want %q", got, "centered text")
	}
}

func TestCenteredWithRubric(t *testing.T) {
	doc, _ := parse(t, "|=rubric=")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, ok := p.Attribute([]byte(attrCentered))
	if !ok {
		t.Error("expected centered attribute on paragraph")
	}
	n := firstOfKind(doc, kindRubricInline)
	if n == nil {
		t.Error("expected rubricInline inside centered paragraph")
	}
}

func TestDropCapParagraph(t *testing.T) {
	doc, source := parse(t, "^dropcap text")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, ok := p.Attribute([]byte(attrDropCap))
	if !ok {
		t.Error("expected dropcap attribute on paragraph")
	}
	got := nodeText(p, source)
	if got != "dropcap text" {
		t.Errorf("paragraph text = %q, want %q", got, "dropcap text")
	}
}

func TestIndentedParagraph(t *testing.T) {
	doc, source := parse(t, "+indented text")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, ok := p.Attribute([]byte(attrIndented))
	if !ok {
		t.Error("expected indent attribute on paragraph")
	}
	got := nodeText(p, source)
	if got != "indented text" {
		t.Errorf("paragraph text = %q, want %q", got, "indented text")
	}
}

func TestRunningHeader(t *testing.T) {
	doc, _ := parse(t, "%header text")
	n := firstOfKind(doc, kindRunningHeaderBlock)
	if n == nil {
		t.Fatal("expected runningHeaderBlock, got nil")
	}
	rh := n.(*runningHeaderBlock)
	if rh.HeaderText != "header text" {
		t.Errorf("HeaderText = %q, want %q", rh.HeaderText, "header text")
	}
}

func TestPageBreak(t *testing.T) {
	doc, _ := parse(t, "===")
	n := firstOfKind(doc, kindPageBreak)
	if n == nil {
		t.Fatal("expected pageBreakBlock, got nil")
	}
}

func TestPageBreakTrailingSpace(t *testing.T) {
	doc, _ := parse(t, "===   ")
	n := firstOfKind(doc, kindPageBreak)
	if n == nil {
		t.Fatal("expected pageBreakBlock with trailing space, got nil")
	}
}

func TestPlainParagraphNoAttributes(t *testing.T) {
	doc, source := parse(t, "plain text")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, centered := p.Attribute([]byte(attrCentered))
	_, dropcap := p.Attribute([]byte(attrDropCap))
	_, indented := p.Attribute([]byte(attrIndented))
	if centered || dropcap || indented {
		t.Error("expected no prefix attributes on plain paragraph")
	}
	got := nodeText(p, source)
	if got != "plain text" {
		t.Errorf("paragraph text = %q, want %q", got, "plain text")
	}
}

// ---- Paragraph transformer with leading directives ----

func TestCenteredWithLeadingDirective(t *testing.T) {
	doc, _ := parse(t, "&{margin а҃}|=text=")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, ok := p.Attribute([]byte(attrCentered))
	if !ok {
		t.Error("expected centered attribute on paragraph with leading directive")
	}
	// The margin directive should be preserved as a pageContextInline.
	pc := firstOfKind(doc, kindPageContextInline)
	if pc == nil {
		t.Error("expected pageContextInline from leading &{margin ...} directive")
	}
}

func TestDropCapWithLeadingDirective(t *testing.T) {
	doc, _ := parse(t, "&{margin а҃}^text")
	p := firstParagraph(doc)
	if p == nil {
		t.Fatal("expected paragraph, got nil")
	}
	_, ok := p.Attribute([]byte(attrDropCap))
	if !ok {
		t.Error("expected dropcap attribute on paragraph with leading directive")
	}
}

// ---- skipDirectives tests ----

func TestSkipDirectivesNone(t *testing.T) {
	got := skipDirectives([]byte("|text"))
	if got != 0 {
		t.Errorf("skipDirectives(|text) = %d, want 0", got)
	}
}

func TestSkipDirectivesSingle(t *testing.T) {
	input := []byte("&{margin а҃}|text")
	got := skipDirectives(input)
	// Should skip past "&{margin а҃}"
	want := len("&{margin а҃}")
	if got != want {
		t.Errorf("skipDirectives = %d, want %d", got, want)
	}
}

func TestSkipDirectivesMultiple(t *testing.T) {
	input := []byte("&{push a b}&{anchor c}|text")
	got := skipDirectives(input)
	want := len("&{push a b}&{anchor c}")
	if got != want {
		t.Errorf("skipDirectives = %d, want %d", got, want)
	}
}

func TestSkipDirectivesUnclosed(t *testing.T) {
	input := []byte("&{unclosed stuff")
	got := skipDirectives(input)
	if got != 0 {
		t.Errorf("skipDirectives(unclosed) = %d, want 0", got)
	}
}

// ---- Edge cases / combinations ----

func TestRubricAdjacentToPunctuation(t *testing.T) {
	doc, source := parse(t, "text =]=")
	n := firstOfKind(doc, kindRubricInline)
	if n == nil {
		t.Fatal("expected rubricInline for =]=, got nil")
	}
	got := nodeText(n, source)
	if got != "]" {
		t.Errorf("rubricInline text = %q, want %q", got, "]")
	}
}

func TestMixedInline(t *testing.T) {
	doc, _ := parse(t, "=rubric= and *plain*")
	rubrics := allOfKind(doc, kindRubricInline)
	plains := allOfKind(doc, kindPlainTextInline)
	if len(rubrics) == 0 {
		t.Error("expected at least one rubricInline")
	}
	if len(plains) == 0 {
		t.Error("expected at least one plainTextInline")
	}
}

func TestNestedIncipitInRubric(t *testing.T) {
	// Incipit inside rubric: the ~ creates its own rubricInline.
	doc, source := parse(t, "=rubric with ~X=")
	rubrics := allOfKind(doc, kindRubricInline)
	// We expect at least two rubricInline nodes: the outer =...= and the inner ~X.
	if len(rubrics) < 2 {
		t.Errorf("expected >=2 rubricInline nodes (outer + incipit), got %d", len(rubrics))
	}
	// The incipit node should contain just "X".
	for _, r := range rubrics {
		txt := nodeText(r, source)
		if txt == "X" {
			return // found it
		}
	}
	t.Error("expected a rubricInline containing just 'X' (incipit)")
}

func TestPageBreakInsideParagraphNotPageBreak(t *testing.T) {
	// === not at column 0 (preceded by text on same paragraph) should NOT be a page break.
	doc, _ := parse(t, "some text ===")
	n := firstOfKind(doc, kindPageBreak)
	if n != nil {
		t.Error("=== inside paragraph should NOT produce a pageBreakBlock")
	}
}

func TestCrossDelimiterNoMatch(t *testing.T) {
	// * and = must not cross-match. The outer * wraps everything as plainTextInline,
	// and the inner =...= is a nested rubricInline.
	doc, source := parse(t, "*plain =rubric= plain*")

	plains := allOfKind(doc, kindPlainTextInline)
	if len(plains) != 1 {
		t.Fatalf("expected 1 plainTextInline, got %d", len(plains))
	}

	rubrics := allOfKind(doc, kindRubricInline)
	if len(rubrics) != 1 {
		t.Fatalf("expected 1 rubricInline, got %d", len(rubrics))
	}

	// The rubricInline must be nested inside the plainTextInline.
	rubric := rubrics[0]
	if rubric.Parent() != plains[0] {
		t.Error("rubricInline should be a child of plainTextInline")
	}

	if got := nodeText(rubric, source); got != "rubric" {
		t.Errorf("rubricInline text = %q, want %q", got, "rubric")
	}
}

// A backslash before a mark makes it text, and is not printed itself. In
// a code span nothing is an escape: the text is as it is written.
func TestBackslashEscape(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"a star", `цѣна̀ 5\* рꙋбле́й`, "цѣна̀ 5* рꙋбле́й"},
		{"a rubric's mark", `а \= б \= в`, "а = б = в"},
		{"an incipit's mark", `\~не кра́сное`, "~не кра́сное"},
		{"a prefix at the head", `\^не бꙋ́квица`, "^не бꙋ́квица"},
		{"a centred line's prefix", `\|не по сре́динѣ`, "|не по сре́динѣ"},
		{"a directive", `\&{anchor c1}`, "&{anchor c1}"},
		{"an expression", `\${page("c1")}`, `${page("c1")}`},
		{"a word in angle brackets", `the text \<sic> stands`, "the text <sic> stands"},
		{"a backslash itself", `а \\ б`, `а \ б`},
		{"before a letter it is a backslash", `а \б`, `а \б`},
		{"in a heading", `## Глава̀ \*`, "Глава̀ *"},
		{"in a code span", "`a \\* b`", `a \* b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Blocks) != 1 {
				t.Fatalf("Parse(%q): %d blocks, want 1", tt.src, len(doc.Blocks))
			}
			b := doc.Blocks[0]
			got := b.Text
			for _, s := range b.Content {
				if s != ir.T(s.Text) {
					t.Errorf("Parse(%q): the span %+v, want plain text", tt.src, s)
				}
				got += s.Text
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// A dash with a space after it, or at the end of its line, is a dash of
// the text, not an indent.
func TestSpacedDashIsText(t *testing.T) {
	for _, src := range []string{
		"во ѻ҆́блацѣхъ - бг҃а и҆ ѻ҆ц҃а̀",
		"на рꙋ́цѣхъ твои́хъ -\nхрⷭ҇та̀ бг҃а",
		"на рꙋ́цѣхъ твои́хъ -",
	} {
		doc, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for _, s := range doc.Blocks[0].Content {
			if s.Indent {
				t.Errorf("Parse(%q): an indent, want none", src)
			}
			text += s.Text
		}
		if !strings.Contains(text, "-") {
			t.Errorf("Parse(%q): text %q, want the dash in it", src, text)
		}
	}

	// The indent itself, at the head of a line and after a space.
	doc, err := Parse("-~Бл҃гослове́нъ\n\n=Ли́къ:= -~Бцⷣе")
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range doc.Blocks {
		indents := 0
		for _, s := range b.Content {
			if s.Indent {
				indents++
			}
		}
		if indents != 1 {
			t.Errorf("block %d: %d indents, want 1", i, indents)
		}
	}
}

func TestIndentMarkerOnlyAtLineStart(t *testing.T) {
	// Hyphen mid-line should NOT become an indent marker
	doc, src := parse(t, "слово-й")
	n := firstOfKind(doc, kindIndentMarker)
	if n != nil {
		t.Fatal("mid-line hyphen should not produce indentMarker")
	}
	// Verify the hyphen is preserved in the text
	text := collectText(doc, src)
	if !strings.Contains(text, "-") {
		t.Errorf("expected hyphen preserved in text, got %q", text)
	}
}

func TestHardBreakTrimsTrailingSpace(t *testing.T) {
	// The space before a backslash break must not reach the renderer: it
	// is a breakpoint, and breaking there leaves an empty line.
	doc, err := Parse("пе́рвый \\\nвторо́й\n")
	if err != nil {
		t.Fatal(err)
	}
	spans := doc.Blocks[0].Content
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if spans[0].Text != "пе́рвый" || !spans[0].Break {
		t.Errorf("first span = %+v, want text %q with Break", spans[0], "пе́рвый")
	}
}

func TestIncipitFlag(t *testing.T) {
	// ~X outside a rubric is an incipit; ~X inside =...= is part of the rubric.
	doc, err := Parse("=Ли́къ:= ~Гдⷭ҇и, =в ~Х=\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Ли́къ:": false, "Г": true, "в ": false, "Х": false}
	for _, s := range doc.Blocks[0].Content {
		if inc, ok := want[s.Text]; ok {
			if !s.Rubric || s.Incipit != inc {
				t.Errorf("span %q: Rubric=%v Incipit=%v, want Rubric=true Incipit=%v", s.Text, s.Rubric, s.Incipit, inc)
			}
			delete(want, s.Text)
		}
	}
	if len(want) > 0 {
		t.Errorf("spans not found: %v", want)
	}
}

func TestLeaderInLink(t *testing.T) {
	// A contents line: the whole line links to the anchor, the incipit
	// included; the leader takes the place of the spaces around it.
	doc, err := Parse("+[~Вели́кая вече́рня &{leader .} ${page(\"vespers\")}.](#vespers) \\\n[~О҆у́трєнѧ &{leader .} ${page(\"matins\")}.](#matins)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 1 {
		t.Fatalf("got %d blocks, want 1 (a leader is not a page context op): %+v", len(doc.Blocks), doc.Blocks)
	}
	type want struct {
		text, leader, expr, link string
		incipit, brk             bool
	}
	wants := []want{
		{text: "В", link: "vespers", incipit: true},
		{text: "ели́кая вече́рня", link: "vespers"},
		{leader: ".", link: "vespers"},
		{expr: `page("vespers")`, link: "vespers"},
		{text: ".", link: "vespers", brk: true},
		{text: "О҆", link: "matins", incipit: true},
		{text: "у́трєнѧ", link: "matins"},
		{leader: ".", link: "matins"},
		{expr: `page("matins")`, link: "matins"},
		{text: ".", link: "matins"},
	}
	spans := doc.Blocks[0].Content
	if len(spans) != len(wants) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(wants), spans)
	}
	for i, w := range wants {
		s := spans[i]
		got := want{s.Text, s.Leader, s.Expr, s.Link, s.Incipit, s.Break}
		if got != w {
			t.Errorf("span %d = %+v, want %+v", i, got, w)
		}
	}
}
