// Package htmlrender renders a document (ir) as HTML.
//
// It is the sibling of pdfrender: the same input, the parsed document and
// the front matter's keys for its expressions, and none of the PDF side's
// machinery. HTML has no page, so what belongs to the page is left out (the
// running header, the page context) or empty (a page number: page() in an
// expression), a page break is a gap, and the typography is a style
// sheet's (CSS), not this code's.
//
// Not rendered yet: leaders.
package htmlrender

import (
	"bufio"
	_ "embed" // for CSS
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/grisha/slavonic-render/internal/assets"
	"github.com/grisha/slavonic-render/internal/expreval"
	"github.com/grisha/slavonic-render/ir"
)

// CSS is the style sheet of the HTML that Render writes. It names the font
// family "Ponomar" and does not load it: the page that uses the sheet
// declares the @font-face, since only the page knows where its font is.
//
//go:embed data/text.css
var CSS string

// exprFuncs are the functions of an expression (${…}) that are this
// renderer's own. HTML has no pages, so the page number of an anchor is
// empty: a line of a table of contents is its title and its link.
var exprFuncs = map[string]any{
	"page": func(string) string { return "" },
}

// Render writes doc to w as a fragment of HTML, one <article>, to be put
// inside a page. frontMatter holds the keys the document's expressions
// (${…}) may name, each the text of its key. An expression that fails (a
// name that is not a key) is an error naming it.
func Render(w io.Writer, doc *ir.Document, frontMatter map[string]string) error {
	r := renderer{w: bufio.NewWriter(w), frontMatter: frontMatter}
	r.w.WriteString("<article class=\"slavonic\">\n")
	for _, b := range doc.Blocks {
		r.block(b)
	}
	r.footnotes(doc.Footnotes)
	r.w.WriteString("</article>\n")
	if err := r.w.Flush(); err != nil {
		return fmt.Errorf("Render: %w", err)
	}
	if r.err != nil {
		return fmt.Errorf("Render: %w", r.err)
	}
	return nil
}

// renderer writes one document. Its writes go to a bufio.Writer, which keeps
// the first error and drops every write after it, so the error is checked
// once, at Flush.
type renderer struct {
	w           *bufio.Writer
	frontMatter map[string]string // the keys an expression may name
	anchor      string            // an anchor waiting for the next element, which takes it as its id
	link        string            // the anchor the open <a> links to; "" when none is open
	class       string            // the class of the open <span>; "" when none is open
	refs        int               // the highest footnote number referenced so far
	err         error             // the first ornament that could not be written, or expression that failed
}

// block writes one block. The kinds without a case are the page's (running
// header, page-context op).
func (r *renderer) block(b ir.Block) {
	switch b.Kind {
	case ir.BlockAnchor:
		r.anchor = b.Text
	case ir.BlockImage:
		r.ornament(b.Text)
	case ir.BlockPageBreak:
		// Where the PDF begins a new page: a gap on a screen, a new page
		// in print, both the style sheet's. It takes no anchor, which
		// waits for the element after it, as at an ornament.
		r.w.WriteString("<hr class=\"page\">\n")
	case ir.BlockParagraph, ir.BlockCentered, ir.BlockDropCap:
		r.open("p", paragraphClass(b))
		r.spans(b)
		r.w.WriteString("</p>\n")
	case ir.BlockHeading1:
		r.heading("h1", b.Text)
	case ir.BlockHeading2:
		r.heading("h2", b.Text)
	case ir.BlockThematicBreak:
		// A vertical space, not a rule: the style sheet's business.
		r.open("hr", "")
		r.w.WriteString("\n")
	}
}

// open writes an opening tag with its class, if any, and the pending anchor,
// if any, as its id.
func (r *renderer) open(tag, class string) {
	fmt.Fprintf(r.w, "<%s", tag)
	if class != "" {
		fmt.Fprintf(r.w, ` class="%s"`, class)
	}
	if r.anchor != "" {
		fmt.Fprintf(r.w, ` id="%s"`, html.EscapeString(r.anchor))
		r.anchor = ""
	}
	r.w.WriteString(">")
}

// ornament writes the ornament an image block names as an <svg> element:
// its source (assets) from the <svg tag on, so without the XML
// declaration the file opens with. The class is for the style sheet, which
// gives the ornament its size and, since the black of the source becomes
// currentColor, its colour. An ornament is decoration: a screen reader is
// told to pass it by, and it takes no anchor, which waits for the element
// after it (as in the PDF).
func (r *renderer) ornament(name string) {
	source, err := assets.SVG(name)
	_, svg, found := strings.Cut(source, "<svg")
	if err == nil && !found {
		err = fmt.Errorf("ornament %q has no <svg", name)
	}
	if err != nil {
		if r.err == nil {
			r.err = fmt.Errorf("renderer.ornament: %w", err)
		}
		return
	}
	svg = strings.ReplaceAll(svg, `fill="#000000"`, `fill="currentColor"`)
	// What follows "<svg" in the source begins with the space before its
	// first attribute, and ends with the file's last newline.
	r.w.WriteString(`<svg class="ornament" aria-hidden="true"`)
	r.w.WriteString(strings.TrimRight(svg, "\n"))
	r.w.WriteString("\n")
}

func (r *renderer) heading(tag, text string) {
	r.open(tag, "")
	fmt.Fprintf(r.w, "%s</%s>\n", html.EscapeString(text), tag)
}

// paragraphClass returns the classes of a paragraph's <p>: its kind, then
// its flags.
func paragraphClass(b ir.Block) string {
	var classes []string
	switch b.Kind {
	case ir.BlockCentered:
		classes = append(classes, "centered")
	case ir.BlockDropCap:
		classes = append(classes, "dropcap")
	}
	if b.Indented {
		classes = append(classes, "indented")
	}
	if b.Minor {
		classes = append(classes, "minor")
	}
	return strings.Join(classes, " ")
}

// spans writes a paragraph's content: its spans, and its margin notes in
// the text, each where it stands. Neighbouring spans of one class share a
// <span>, and neighbouring spans of one link an <a>, with the <span>s
// inside it.
//
// A note's CharPos is the number of runes of text before it, counted as the
// parser counts them: over the spans that have text, and nothing for an
// indent, a leader, a footnote reference or an expression. So a note may
// stand inside a span, whose text is then written in two pieces around it.
func (r *renderer) spans(b ir.Block) {
	notes := b.Notes
	written := 0 // the runes of text written so far
	for _, s := range b.Content {
		switch {
		case s.Indent:
			r.setClass("")
			r.w.WriteString(`<span class="indent"></span>`)
		case s.FootnoteRef > 0:
			r.reference(s.FootnoteRef)
		case s.Expr != "":
			// What an expression gives is not text the parser counted, so
			// it is not added to written: the notes stay where they stand.
			// A note at this very place goes before it, as before a word.
			for len(notes) > 0 && notes[0].CharPos <= written {
				r.note(notes[0].Text, b.Kind == ir.BlockDropCap && written == 0)
				notes = notes[1:]
			}
			text, err := expreval.Eval(s.Expr, r.frontMatter, exprFuncs)
			if err != nil && r.err == nil {
				r.err = fmt.Errorf("renderer.spans: ${%s}: %w", s.Expr, err)
			}
			r.text(s, text)
		case s.Text != "":
			text := []rune(s.Text)
			for len(notes) > 0 && notes[0].CharPos < written+len(text) {
				n := max(notes[0].CharPos-written, 0)
				r.text(s, string(text[:n]))
				written += n
				r.note(notes[0].Text, b.Kind == ir.BlockDropCap && written == 0)
				text, notes = text[n:], notes[1:]
			}
			r.text(s, string(text))
			written += len(text)
		}
		// A span that writes nothing (a leader, an expression that is
		// empty, as a page number is) still has its break: the parser puts
		// a forced break on whatever span comes last before it.
		if s.Break {
			r.closeInline()
			r.w.WriteString("<br>\n")
		}
	}
	// The notes that stand after the last of the text.
	for _, n := range notes {
		r.note(n.Text, false)
	}
	r.closeInline()
}

// text writes a piece of a span's text, inside the span's link and class.
func (r *renderer) text(s ir.Span, text string) {
	if text == "" {
		return
	}
	r.setLink(s.Link)
	r.setClass(spanClass(s))
	r.w.WriteString(html.EscapeString(text))
}

// note writes a margin note (a verse number, a label of the stichera). A
// page of HTML has no margin, so the note stands in the text, before the
// word it marks, and ends in a no-break space that keeps it with that
// word: it never ends a line alone. lead marks the note that stands before
// a drop cap, for the style sheet to take out of the first line, or the
// note's first letter would be the drop cap.
func (r *renderer) note(text string, lead bool) {
	r.setClass("")
	class := "note"
	if lead {
		class = "note lead"
	}
	fmt.Fprintf(r.w, `<span class="%s">%s&nbsp;</span>`, class, html.EscapeString(text))
}

// reference writes a footnote reference: the note's number, raised, a link
// to the note at the end of the document. It stands outside the <span> and
// the <a> that are open, so that its size is the same in a rubric and out
// of one, and a link is not inside a link; the text after it opens them
// again.
//
// The first reference to a note takes an id, for the note to link back to;
// a later one to the same note does not, since an id stands once in a page.
// The parser numbers the notes in order of first reference, so the first
// reference to a note is the one whose number is higher than all before.
func (r *renderer) reference(num int) {
	r.closeInline()
	r.w.WriteString("<sup")
	if num > r.refs {
		fmt.Fprintf(r.w, ` id="footnote-ref-%d"`, num)
		r.refs = num
	}
	fmt.Fprintf(r.w, `><a href="#footnote-%d">%d</a></sup>`, num, num)
}

// footnotes writes the notes, all of them after the text, since a page of
// HTML has no foot: each a paragraph that opens with its number, which is
// the link back to its reference. Nothing is written when there are none.
func (r *renderer) footnotes(notes []ir.FootnoteDef) {
	if len(notes) == 0 {
		return
	}
	r.w.WriteString("<aside class=\"footnotes\">\n")
	for _, n := range notes {
		fmt.Fprintf(r.w, `<p id="footnote-%d"><a href="#footnote-ref-%d">%d.</a> `, n.Num, n.Num, n.Num)
		r.spans(ir.Block{Content: n.Content})
		r.w.WriteString("</p>\n")
	}
	r.w.WriteString("</aside>\n")
}

// closeInline closes the open <span> and the open <a>, if any: at a forced
// break, before a footnote reference and at the end of the paragraph.
func (r *renderer) closeInline() {
	r.setClass("")
	r.setLink("")
}

// spanClass returns the class of a span of text: one of three, since an
// incipit is a rubric that keeps the body size and a rubric is never plain.
func spanClass(s ir.Span) string {
	switch {
	case s.Incipit:
		return "incipit"
	case s.Rubric:
		return "rubric"
	case s.Plain:
		return "plain"
	}
	return ""
}

// setLink closes the open <a>, if any, and opens one to link, if not empty;
// it does nothing when link is the one already open.
func (r *renderer) setLink(link string) {
	if link == r.link {
		return
	}
	r.setClass("")
	if r.link != "" {
		r.w.WriteString("</a>")
	}
	if link != "" {
		fmt.Fprintf(r.w, `<a href="#%s">`, html.EscapeString(link))
	}
	r.link = link
}

// setClass does for the <span> of a class what setLink does for the <a>.
func (r *renderer) setClass(class string) {
	if class == r.class {
		return
	}
	if r.class != "" {
		r.w.WriteString("</span>")
	}
	if class != "" {
		fmt.Fprintf(r.w, `<span class="%s">`, class)
	}
	r.class = class
}
