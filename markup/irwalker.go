package markup

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"

	"github.com/grisha/slavonic-render/ir"
)

// walker walks a goldmark AST and produces a ir.Document (an intermediate
// representation). It has no rendering dependencies — it is a pure parser
// that converts AST nodes into IR blocks.
type walker struct {
	source []byte // goldmark source text for segment resolution

	// Footnote state: definitions collected from [^id]: paragraphs,
	// display numbers assigned in order of first [^id] reference.
	footnoteDefs  map[string][]ir.Span // ID -> content spans
	footnoteOrder []string             // IDs in order of first reference
	footnoteNums  map[string]int       // ID -> display number
}

// walk traverses the AST and returns a Document containing all blocks.
func (w *walker) walk(doc ast.Node, source []byte) (*ir.Document, error) {
	w.source = source
	w.footnoteDefs = make(map[string][]ir.Span)
	w.footnoteNums = make(map[string]int)

	if err := checkInline(doc, source); err != nil {
		return nil, err
	}

	var d ir.Document
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		blocks, err := w.convertBlock(child)
		if err != nil {
			return nil, err
		}
		d.Blocks = append(d.Blocks, blocks...)
	}

	// Assemble footnotes in order of first reference.
	for _, id := range w.footnoteOrder {
		content, ok := w.footnoteDefs[id]
		if !ok {
			fmt.Fprintf(os.Stderr, "WARNING: footnote [^%s] referenced but not defined\n", id)
			continue
		}
		d.Footnotes = append(d.Footnotes, ir.FootnoteDef{
			ID:      id,
			Num:     w.footnoteNums[id],
			Content: content,
		})
	}

	return &d, nil
}

// convertBlock dispatches a single block-level node and returns one or more
// IR blocks. Most nodes produce exactly one block; page context directives
// embedded in inline content may produce additional blocks.
func (w *walker) convertBlock(n ast.Node) ([]ir.Block, error) {
	switch v := n.(type) {
	case *ast.Heading:
		return w.convertHeading(v)
	case *ast.Paragraph:
		return w.convertParagraph(v)
	case *runningHeaderBlock:
		return []ir.Block{{Kind: ir.BlockRunningHeader, Text: v.HeaderText}}, nil
	case *pageBreakBlock:
		return []ir.Block{{Kind: ir.BlockPageBreak}}, nil
	case *ast.Blockquote:
		return w.convertMinorBlock(v)
	case *ast.ThematicBreak:
		return []ir.Block{{Kind: ir.BlockThematicBreak}}, nil
	default:
		// A list, a code block, an HTML block: Markdown this markup has no
		// style for. A list is the one met by accident: a line beginning
		// "+ ", "- ", "* " or "1. " is one.
		return nil, fmt.Errorf("convertBlock: a Markdown %s is not supported: %q", n.Kind(), w.firstLine(n))
	}
}

// convertHeading handles # and ## headings.
func (w *walker) convertHeading(h *ast.Heading) ([]ir.Block, error) {
	var blocks []ir.Block

	// Collect any page context directives from inline children first.
	blocks = append(blocks, w.collectInlineDirectives(h)...)

	text := w.inlineText(h)
	if text == "" {
		return blocks, nil
	}

	kind := ir.BlockHeading2
	if h.Level == 1 {
		kind = ir.BlockHeading1
	}
	blocks = append(blocks, ir.Block{Kind: kind, Text: text})
	return blocks, nil
}

// convertParagraph handles regular paragraphs, including attributed ones.
func (w *walker) convertParagraph(p *ast.Paragraph) ([]ir.Block, error) {
	// Footnote definition: consume and store, emit no blocks.
	if fnID, ok := p.Attribute([]byte(attrFootnoteDef)); ok {
		id := fnID.(string)
		if _, dup := w.footnoteDefs[id]; dup {
			fmt.Fprintf(os.Stderr, "WARNING: duplicate footnote definition [^%s], using first\n", id)
			return nil, nil
		}
		spans, _ := w.extractSpansWithNotes(p)
		w.footnoteDefs[id] = spans
		return nil, nil
	}

	_, centered := p.Attribute([]byte(attrCentered))
	_, dropcap := p.Attribute([]byte(attrDropCap))
	_, indented := p.Attribute([]byte(attrIndented))

	// An image block names its ornament ("panel"); a renderer finds the
	// file. The image is the whole paragraph: checkInline saw to that.
	if img, ok := p.FirstChild().(*ast.Image); ok {
		return []ir.Block{{Kind: ir.BlockImage, Text: string(img.Destination)}}, nil
	}

	// Collect page context directives first (they become separate blocks).
	directives := w.collectInlineDirectives(p)

	// Extract spans and margin notes from inline content.
	spans, notes := w.extractSpansWithNotes(p)
	if noText(spans) {
		return directives, nil
	}

	var kind ir.BlockKind
	switch {
	case dropcap:
		kind = ir.BlockDropCap
	case centered:
		kind = ir.BlockCentered
	default:
		kind = ir.BlockParagraph
	}

	block := ir.Block{
		Kind:     kind,
		Content:  spans,
		Indented: indented,
		Notes:    notes,
	}

	return append(directives, block), nil
}

// convertMinorBlock handles blockquote nodes (> prefix), treating them as
// minor paragraphs (slightly smaller text). Inner paragraphs retain their
// own prefixes (|, ^, +), so combinations like "> | text" work naturally.
func (w *walker) convertMinorBlock(bq *ast.Blockquote) ([]ir.Block, error) {
	var blocks []ir.Block
	for child := bq.FirstChild(); child != nil; child = child.NextSibling() {
		childBlocks, err := w.convertBlock(child)
		if err != nil {
			return nil, err
		}
		for i := range childBlocks {
			childBlocks[i].Minor = true
		}
		blocks = append(blocks, childBlocks...)
	}
	return blocks, nil
}

// ---- Text extraction helpers ----

// inlineText walks inline children and concatenates their text content.
// Rubric markers are stripped — this returns flat text.
func (w *walker) inlineText(n ast.Node) string {
	var sb strings.Builder
	w.collectText(n, &sb)
	return strings.TrimSpace(sb.String())
}

func (w *walker) collectText(n ast.Node, sb *strings.Builder) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Text:
			sb.WriteString(w.textOf(c))
			if c.SoftLineBreak() {
				sb.WriteByte(' ')
			}
		case *expressionInline:
			// Store raw expression as placeholder; renderer resolves later.
			sb.WriteString("${" + c.Expr + "}")
		case *pageContextInline:
			// Directives produce no text; collected separately.
		default:
			w.collectText(c, sb)
		}
	}
}

// textOf is the text of a text node: its source, without the backslash of
// an escape. The parser has already kept an escaped mark (\*, \=) from
// being read as markup; the backslash is still in the source. A raw node,
// the text of a code span, is as it is written.
func (w *walker) textOf(t *ast.Text) string {
	source := t.Segment.Value(w.source)
	if t.IsRaw() {
		return string(source)
	}
	return string(util.UnescapePunctuations(source))
}

// extractSpansWithNotes walks inline children and builds spans + margin notes.
func (w *walker) extractSpansWithNotes(n ast.Node) ([]ir.Span, []ir.MarginNote) {
	var spans []ir.Span
	var notes []ir.MarginNote
	runeCount := 0
	w.collectSpansWithNotes(n, false, false, "", &spans, &runeCount, &notes)
	return spans, notes
}

// collectSpansWithNotes appends the spans of n's inline children. inRubric,
// inPlain and link are what the enclosing markup sets on every span inside
// it: =rubric=, *plain* and [a link](#anchor).
func (w *walker) collectSpansWithNotes(n ast.Node, inRubric, inPlain bool, link string, spans *[]ir.Span, runeCount *int, notes *[]ir.MarginNote) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Text:
			text := w.textOf(c)
			if c.SoftLineBreak() {
				text += " "
			}
			if c.HardLineBreak() {
				// Goldmark keeps the space before a backslash break
				// ("word \"). Left in, it is a breakpoint: when the text
				// just fills the line, the line breaks at that space and
				// the forced break that follows makes an empty line. TeX's
				// \\ does \unskip for the same reason.
				text = strings.TrimRight(text, " \t")
			}
			if n := len(*spans); n > 0 && (*spans)[n-1].Leader != "" {
				// The leader is the space between the text before it
				// and the text after it; see trimBeforeLeader.
				text = strings.TrimLeft(text, " \t")
			}
			if text != "" {
				*runeCount += len([]rune(text))
				span := ir.Span{Text: text, Rubric: inRubric, Plain: inPlain, Link: link}
				if c.HardLineBreak() {
					span.Break = true
				}
				*spans = append(*spans, span)
			} else if c.HardLineBreak() {
				if len(*spans) > 0 {
					(*spans)[len(*spans)-1].Break = true
				}
			}
		case *rubricInline:
			// Rubric text is Church Slavonic by definition; reset inPlain.
			start := len(*spans)
			w.collectSpansWithNotes(c, true, false, link, spans, runeCount, notes)
			// An incipit inside a rubric is part of the rubric.
			if c.incipit && !inRubric {
				for i := start; i < len(*spans); i++ {
					(*spans)[i].Incipit = true
				}
			}
		case *plainTextInline:
			w.collectSpansWithNotes(c, inRubric, true, link, spans, runeCount, notes)
		case *ast.Link:
			// [text](#anchor): every span inside links to the anchor.
			dest := strings.TrimPrefix(string(c.Destination), "#")
			w.collectSpansWithNotes(c, inRubric, inPlain, dest, spans, runeCount, notes)
		case *indentMarker:
			*spans = append(*spans, ir.Span{Indent: true})
		case *footnoteRefInline:
			// Assign display number on first reference.
			num, ok := w.footnoteNums[c.ID]
			if !ok {
				num = len(w.footnoteOrder) + 1
				w.footnoteNums[c.ID] = num
				w.footnoteOrder = append(w.footnoteOrder, c.ID)
			}
			*spans = append(*spans, ir.Span{FootnoteRef: num})
		case *expressionInline:
			// Store the raw expression in the span for later resolution.
			span := ir.Span{Expr: c.Expr, Rubric: inRubric, Plain: inPlain, Link: link}
			*spans = append(*spans, span)
		case *pageContextInline:
			switch c.Op {
			case "margin":
				noteText := c.Key
				if c.Value != "" {
					noteText = c.Key + " " + c.Value
				}
				*notes = append(*notes, ir.MarginNote{
					CharPos: *runeCount,
					Text:    noteText,
				})
			case "leader":
				trimBeforeLeader(spans, runeCount)
				*spans = append(*spans, ir.Span{Leader: c.Key, Rubric: inRubric, Plain: inPlain, Link: link})
			}
			// Other directives handled by collectInlineDirectives
		default:
			w.collectSpansWithNotes(c, inRubric, inPlain, link, spans, runeCount, notes)
		}
	}
}

// noText reports whether the spans are nothing, or spaces alone: what is
// left of a paragraph of directives, which makes no block of its own.
func noText(spans []ir.Span) bool {
	for _, s := range spans {
		if s != ir.T(s.Text) || strings.Trim(s.Text, " \t") != "" {
			return false
		}
	}
	return true
}

// trimBeforeLeader removes the space at the end of the last span, before a
// leader, as TeX's \unskip does: the leader is the space between the text
// before it and the text after it, and a space beside it would be a
// breakpoint and a gap that differs from line to line. A span left with no
// text is dropped.
func trimBeforeLeader(spans *[]ir.Span, runeCount *int) {
	n := len(*spans)
	if n == 0 || (*spans)[n-1].Text == "" {
		return
	}
	last := &(*spans)[n-1]
	trimmed := strings.TrimRight(last.Text, " \t")
	*runeCount -= len([]rune(last.Text)) - len([]rune(trimmed))
	last.Text = trimmed
	if trimmed == "" && !last.Break {
		*spans = (*spans)[:n-1]
	}
}

// collectInlineDirectives scans inline children for page context and anchor
// directives, returning them as separate IR blocks. Margin notes and leaders
// are excluded (they are part of the paragraph: its Notes, its spans).
func (w *walker) collectInlineDirectives(n ast.Node) []ir.Block {
	var blocks []ir.Block
	w.walkDirectives(n, &blocks)
	return blocks
}

func (w *walker) walkDirectives(n ast.Node, blocks *[]ir.Block) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if c, ok := child.(*pageContextInline); ok {
			switch c.Op {
			case "anchor":
				*blocks = append(*blocks, ir.Block{Kind: ir.BlockAnchor, Text: c.Key})
			case "push":
				*blocks = append(*blocks, ir.Block{
					Kind:  ir.BlockPageContextOp,
					Key:   c.Key,
					Value: c.Value,
				})
			}
			// A margin note and a leader are in their paragraph's spans;
			// checkInline has refused any other name.
		} else {
			w.walkDirectives(child, blocks)
		}
	}
}

// checkInline refuses the inline markup the walker would otherwise drop
// without a word: a directive of a name that is not one (&{anhcor c1}) or
// with a part too many or too few, an image that is not alone in its
// paragraph (the paragraph becomes the ornament, and its text is lost),
// a link that does not go to an anchor, and Markdown's autolink and inline HTML, <https://…> and <br>, which
// have no span and made nothing: a word in angle brackets that is text
// is written \<word>.
func checkInline(doc ast.Node, source []byte) error {
	return ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *pageContextInline:
			var wrong string
			switch v.Op {
			case "margin":
			case "anchor":
				if v.Value != "" {
					wrong = "an anchor has one name"
				}
			case "leader":
				if v.Value != "" {
					wrong = "a leader has one glyph"
				}
			case "push":
				if v.Value == "" {
					wrong = "a push has a key and a value"
				}
			default:
				wrong = "not a directive (anchor, push, margin, leader)"
			}
			if wrong != "" {
				src := strings.TrimSpace(v.Op + " " + v.Key + " " + v.Value)
				return ast.WalkStop, fmt.Errorf("checkInline: &{%s}: %s", src, wrong)
			}
		case *ast.Image:
			if p, ok := v.Parent().(*ast.Paragraph); !ok || p.ChildCount() != 1 {
				return ast.WalkStop, fmt.Errorf("checkInline: ![](%s): an ornament is a paragraph of its own", v.Destination)
			}
		case *ast.Link:
			// A link goes to an anchor of the document; one out of it
			// would be made a link to an anchor of that name, and dead.
			if !bytes.HasPrefix(v.Destination, []byte("#")) {
				return ast.WalkStop, fmt.Errorf("checkInline: [](%s): a link goes to an anchor of the document, [text](#name)", v.Destination)
			}
		case *ast.AutoLink:
			return ast.WalkStop, fmt.Errorf("checkInline: a Markdown autolink is not supported: <%s>", v.URL(source))
		case *ast.RawHTML:
			return ast.WalkStop, fmt.Errorf("checkInline: inline HTML is not supported: %s", v.Segments.Value(source))
		}
		return ast.WalkContinue, nil
	})
}

// firstLine is the first line of the source under a block, for an error
// to quote.
func (w *walker) firstLine(n ast.Node) string {
	if n.Type() == ast.TypeInline {
		return ""
	}
	if lines := n.Lines(); lines != nil && lines.Len() > 0 {
		seg := lines.At(0)
		return strings.TrimSpace(string(seg.Value(w.source)))
	}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if line := w.firstLine(child); line != "" {
			return line
		}
	}
	return ""
}
