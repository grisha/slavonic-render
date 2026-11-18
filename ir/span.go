// Package ir is the intermediate representation of a document: the model the
// Markdown parser (markup) produces and the renderers consume.
//
// It is pure data: blocks (paragraphs, headings, drop caps, directives) made
// of spans of text with inline formatting (rubric, incipit, plain face),
// margin notes and footnotes. It has no rendering dependencies; conversion
// to PDF primitives is the pdfrender package's.
//
// Example usage:
//
//	para := ir.Paragraph{
//	    Content: []ir.Span{
//	        ir.T("Помѧни́ Го́споди "),
//	        ir.R("имⷬ҇къ"),
//	        ir.T(" и҆ поми́лꙋй."),
//	    },
//	}
//
//	// Centered rubric:
//	para := ir.Paragraph{
//	    Centered: true,
//	    Content: []ir.Span{
//	        ir.R("Глас҃ а҃."),
//	    },
//	}
package ir

// Span represents a contiguous run of text with uniform formatting.
type Span struct {
	Text        string `json:"text,omitempty"`        // The text content
	Rubric      bool   `json:"rubric,omitempty"`      // True if this span should be rendered in rubric (red) color
	Incipit     bool   `json:"incipit,omitempty"`     // True for the red initial of ~X outside a rubric: it keeps the body size
	Plain       bool   `json:"plain,omitempty"`       // True if this span uses the modern/plain typeface
	Break       bool   `json:"break,omitempty"`       // True if a forced line break follows this span
	Indent      bool   `json:"indent,omitempty"`      // True if this span is preceded by a horizontal indent
	Leader      string `json:"leader,omitempty"`      // If non-empty, this span is a leader: space that takes what is left of the line, filled with this glyph repeated (Text is empty)
	Link        string `json:"link,omitempty"`        // If non-empty, this span is a PDF hyperlink to the named destination
	Expr        string `json:"expr,omitempty"`        // If non-empty, this span holds an unresolved expression (Text is empty)
	FootnoteRef int    `json:"footnoteRef,omitempty"` // If >0, render as superscript footnote reference with this display number
}

// T creates a plain text span.
func T(s string) Span {
	return Span{Text: s}
}

// R creates a rubric (red) text span.
func R(s string) Span {
	return Span{Text: s, Rubric: true}
}

// MarginNote represents a margin note to be placed at a specific character
// position in the formatted text.
type MarginNote struct {
	CharPos int    `json:"charPos"`        // character position in the cleaned text (rune offset)
	Text    string `json:"text,omitempty"` // the margin note content (e.g., "сⷯа҃")
}

// Paragraph represents a collection of formatted spans with optional
// paragraph-level layout (centered or indented).
type Paragraph struct {
	Content  []Span // Text content with inline formatting
	Centered bool   // True for centered paragraphs (| prefix in markup)
	Indented bool   // True for indented paragraphs (+ prefix in markup)
	Minor    bool   // True for minor paragraphs (> prefix in markup, slightly smaller text)
}

// PlainText returns the concatenated text of all spans (for processing).
func (p *Paragraph) PlainText() string {
	var result string
	for _, span := range p.Content {
		result += span.Text
	}
	return result
}
