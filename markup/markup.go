// Package markup provides goldmark extensions for Church Slavonic liturgical markup.
//
// Custom syntax:
//
// Inline:
//   - =text=       → inline rubric (red text)
//   - *text*       → inline plain/instruction text (modern typeface, smaller, red)
//   - ~text        → incipit marker: first grapheme cluster after ~ rendered in rubric (red)
//   - -text        → inline indent: inserts ~2 letter-widths of horizontal space ("word - word" is a dash)
//   - ${expr}      → inline expression (evaluated at render time; deferred if unresolvable)
//   - [^id]        → footnote reference (superscript number assigned by order of first reference)
//
// Directives, &{name …}; another name, or a part too many or too few, is
// an error:
//   - &{margin text}     → margin note, in the outer margin at its line
//   - &{leader .}        → leader: what is left of the line, filled with the glyph
//   - &{anchor name}     → a named place, for ${page("name")} and [text](#name)
//   - &{push key value}  → append value to the key's list in the per-page context
//
// An anchor and a push are blocks, not text: a line of them above a
// paragraph is a paragraph of its own, with or without a blank line under
// it, so the paragraph keeps its prefix.
//
// An ornament:
//   - ![](name)    → the ornament of that name, a paragraph of its own
//
// Paragraph prefixes (applied by paragraph transformer):
//   - |text        → centered paragraph (inline markup like =rubric= and *plain* works inside)
//   - ^text        → drop cap paragraph (first letter becomes decorative initial)
//   - +text        → indented paragraph (left indent of ~2 letter-widths, all lines)
//   - %text        → running header directive (sets the page running header, no visual output)
//
// Minor paragraphs (uses goldmark's blockquote syntax):
//   - > text       → minor paragraph (slightly smaller text)
//   - > | text     → minor + centered (prefixes combinable inside blockquote)
//   - > +text      → minor + indented (no space: "+ text" is a Markdown list)
//   - > ^ text     → minor + drop cap
//
// Block-level:
//   - [^id]: text  → footnote definition (inline markup works inside)
//   - ===          → page break (forces a new page)
//
// Of Markdown's own, headings, paragraphs, block quotes, ***, the hard
// break (\), the link to an anchor and the code span (its text as it is
// written, no markup read in it) have a style. A list, a code block, an
// HTML block, a link out of the document, an autolink (<https://…>) and
// inline HTML (<br>) are errors.
// There is no bold or italic: * is the plain style, and _text_ prints its
// text unchanged. A backslash before a mark of punctuation makes it text
// (\*, \=, \<, and \^ at the head of a paragraph).
package markup

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/util"
)

// churchSlavonicExtension is a goldmark extension that adds Church Slavonic
// inline rubrics (=text=) and paragraph prefix handling (|, ^, ~).
type churchSlavonicExtension struct{}

func (e *churchSlavonicExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&footnoteRefInlineParser{}, 199),                         // must beat goldmark's link parser (200) to intercept [^
			util.Prioritized(&delimiterInlineParser{'*', plainDelimiter, false}, 499), // must be < 500 to beat goldmark's built-in *...* parser
			util.Prioritized(&delimiterInlineParser{'=', rubricDelimiter, true}, 500), // priorities don't matter for the rest since they use unique trigger bytes
			util.Prioritized(&incipitInlineParser{}, 500),
			util.Prioritized(&indentInlineParser{}, 500),
			util.Prioritized(&expressionInlineParser{}, 500),
			util.Prioritized(&pageContextInlineParser{}, 500),
		),
		parser.WithParagraphTransformers(
			util.Prioritized(&directiveLineParagraphTransformer{}, 48), // first: the others then see the paragraph's own first line
			util.Prioritized(&footnoteDefParagraphTransformer{}, 49),   // before prefix transformer
			util.Prioritized(&prefixParagraphTransformer{}, 50),
		),
	)
}
