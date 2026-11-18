package markup

import (
	"bytes"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Paragraph attribute keys set by the prefix transformer.
const (
	attrCentered    = "centered"    // | prefix
	attrDropCap     = "dropcap"     // ^ prefix
	attrIndented    = "indent"      // + prefix
	attrFootnoteDef = "footnote-id" // [^id]: prefix
)

// ---- Generic paired-delimiter parser (=rubric=, *plain*) ----

// delimiterProcessor is a generic parser.DelimiterProcessor for paired
// delimiters like =rubric= and *plain*. It matches a single delimiter byte
// and wraps matched content in the node returned by newNode.
type delimiterProcessor struct {
	delim   byte
	newNode func() ast.Node
}

// Satisfy parser.DelimiterProcessor
func (p *delimiterProcessor) IsDelimiter(b byte) bool { return b == p.delim }
func (p *delimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}
func (p *delimiterProcessor) OnMatch(consumes int) ast.Node { return p.newNode() }

var (
	rubricDelimiter = &delimiterProcessor{'=', func() ast.Node { return &rubricInline{} }}
	plainDelimiter  = &delimiterProcessor{'*', func() ast.Node { return &plainTextInline{} }}
)

// delimiterInlineParser is a generic inline parser for paired delimiters.
// It triggers on a single byte and uses a delimiterProcessor to match pairs.
type delimiterInlineParser struct {
	trigger         byte
	processor       *delimiterProcessor
	alwaysOpenClose bool // bypass CommonMark flanking rules (safe for = since it never appears in CU text)
}

// Satisfy parser.InlineParser
func (p *delimiterInlineParser) Trigger() []byte { return []byte{p.trigger} }
func (p *delimiterInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	node := parser.ScanDelimiter(line, before, 1, p.processor)
	if node == nil {
		return nil
	}
	if p.alwaysOpenClose {
		node.CanOpen = true
		node.CanClose = true
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}

// ---- Inline parser for ~X (incipit: first grapheme in rubric) ----

type incipitInlineParser struct{}

func (p *incipitInlineParser) Trigger() []byte { return []byte{'~'} }
func (p *incipitInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) == 0 || line[0] != '~' {
		return nil
	}

	// Template functions such as numberVerses put &{margin ...} directives
	// in front of the first word, so "~{{ include ... }}" arrives here as
	// "~&{margin в҃}Гдⷭ҇и". Those directives become their own nodes ahead of
	// the incipit, and the incipit colours the first grapheme after them.
	var directives []ast.Node
	pos := 1 // skip ~
	for {
		d, n := parseDirective(line[pos:])
		if d == nil {
			break
		}
		directives = append(directives, d)
		pos += n
	}

	gr := uniseg.NewGraphemes(string(line[pos:]))
	if !gr.Next() {
		return nil
	}
	graphemeLen := len(gr.Str())

	for _, d := range directives {
		parent.AppendChild(parent, d)
	}

	// Create a rubricInline node containing just the grapheme
	node := &rubricInline{incipit: true}
	innerSeg := segment.WithStart(segment.Start + pos)
	innerSeg = innerSeg.WithStop(segment.Start + pos + graphemeLen)
	node.AppendChild(node, ast.NewRawTextSegment(innerSeg))
	block.Advance(pos + graphemeLen) // consume ~, directives, grapheme
	return node
}

// ---- Inline parser for - (indent/horizontal space) ----

type indentInlineParser struct{}

func (p *indentInlineParser) Trigger() []byte {
	return []byte{'-'}
}
func (p *indentInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	if !unicode.IsSpace(block.PrecendingCharacter()) {
		return nil
	}
	line, _ := block.PeekLine()
	// The indent's text stands straight after it (-text). With a space
	// after it, or the end of the line, the - is a dash of the text.
	if len(line) < 2 || line[0] != '-' || unicode.IsSpace(rune(line[1])) {
		return nil
	}
	block.Advance(1) // consume -
	return &indentMarker{}
}

// ---- Inline parser for ${...} expressions ----

type expressionInlineParser struct{}

func (p *expressionInlineParser) Trigger() []byte { return []byte{'$'} }
func (p *expressionInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 3 || line[0] != '$' || line[1] != '{' {
		return nil
	}
	// Find matching closing brace, respecting nested {}.
	depth := 1
	end := -1
	for i := 2; i < len(line); i++ {
		if line[i] == '{' {
			depth++
		}
		if line[i] == '}' {
			depth--
			if depth == 0 {
				end = i - 2
				break
			}
		}
	}
	if end < 0 {
		return nil // no matching closing brace on this line
	}
	expr := string(line[2 : 2+end])
	if expr == "" {
		return nil
	}
	block.Advance(2 + end + 1) // consume ${expr}
	return &expressionInline{Expr: expr}
}

// ---- Inline parser for &{...} page context directives ----

type pageContextInlineParser struct{}

func (p *pageContextInlineParser) Trigger() []byte { return []byte{'&'} }
func (p *pageContextInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	node, n := parseDirective(line)
	if node == nil {
		return nil
	}
	block.Advance(n) // consume &{...}
	return node
}

// parseDirective parses a &{op key [value]} directive at the start of line.
// It returns the node and the number of bytes the directive occupies, or
// nil and 0 if line does not start with a well-formed directive.
func parseDirective(line []byte) (*pageContextInline, int) {
	if len(line) < 3 || line[0] != '&' || line[1] != '{' {
		return nil, 0
	}
	end := bytes.IndexByte(line[2:], '}')
	if end < 0 {
		return nil, 0 // no closing brace on this line
	}
	content := strings.TrimSpace(string(line[2 : 2+end]))
	if content == "" {
		return nil, 0
	}
	// Parse "op key [value]" — value may contain spaces.
	parts := strings.SplitN(content, " ", 3)
	if len(parts) < 2 {
		return nil, 0 // malformed
	}
	node := &pageContextInline{
		Op:  parts[0],
		Key: parts[1],
	}
	if len(parts) == 3 {
		node.Value = parts[2]
	}
	return node, 2 + end + 1
}

// ---- Paragraph transformer for lines of block directives ----

// directiveLineParagraphTransformer makes a paragraph of its own of each
// leading line that holds nothing but anchors and pushes, as a blank line
// under it would:
//
//	&{push psalms і҃}
//	^Text
//
// The anchor and the push are blocks of the document, not text of the
// paragraph: left as its first line, they would put its prefix on the
// second, where it is not one.
type directiveLineParagraphTransformer struct{}

func (t *directiveLineParagraphTransformer) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	lines := node.Lines()
	// The last line stays whatever it holds: a paragraph of directives
	// alone is already its own.
	n := 0
	for ; n < lines.Len()-1; n++ {
		seg := lines.At(n)
		if !blockDirectivesOnly(seg.Value(source)) {
			break
		}
	}
	if n == 0 {
		return
	}
	parent := node.Parent()
	rest := text.NewSegments()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if i >= n {
			rest.Append(seg)
			continue
		}
		// Trimmed as the paragraph parser trims a paragraph it closes;
		// the newline left in would be a space of text.
		seg = seg.TrimLeftSpace(source)
		seg = seg.TrimRightSpace(source)
		own := ast.NewParagraph()
		own.Lines().Append(seg)
		parent.InsertBefore(parent, node, own)
	}
	node.SetLines(rest)
}

// blockDirectivesOnly reports whether line holds one or more &{anchor …}
// and &{push …} directives and nothing else.
func blockDirectivesOnly(line []byte) bool {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return false
	}
	for len(line) > 0 {
		d, n := parseDirective(line)
		if d == nil || (d.Op != "anchor" && d.Op != "push") {
			return false
		}
		line = bytes.TrimLeft(line[n:], " \t")
	}
	return true
}

// ---- Paragraph transformer for line prefixes ----

type prefixParagraphTransformer struct{}

func (t *prefixParagraphTransformer) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	lines := node.Lines()
	if lines.Len() == 0 {
		return
	}

	// Check first character of first line
	firstSeg := lines.At(0)
	firstLine := firstSeg.Value(reader.Source())
	if len(firstLine) == 0 {
		return
	}

	// Check for === page break directive (must start at column 0, single line)
	if firstLine[0] == '=' && lines.Len() == 1 && strings.TrimRight(string(firstLine), " \t\n\r") == "===" {
		parent := node.Parent()
		parent.ReplaceChild(parent, node, &pageBreakBlock{})
		return
	}

	// Skip leading &{...} directives (e.g. margin notes placed before a
	// prefix by template functions like firstVerseAsHeading).
	prefixOffset := skipDirectives(firstLine)
	if prefixOffset >= len(firstLine) {
		return
	}
	prefix := firstLine[prefixOffset]

	switch prefix {
	case '|':
		t.transformCentered(node, lines, prefixOffset)
	case '^':
		t.transformDropCap(node, lines, prefixOffset)
	case '+':
		t.transformIndented(node, lines, prefixOffset)
	case '%':
		t.transformRunningHeader(node, reader, lines, prefixOffset)
	}
}

func (t *prefixParagraphTransformer) transformCentered(node *ast.Paragraph, lines *text.Segments, prefixOffset int) {
	// Strip | prefix from first line, keep as ast.Paragraph for inline parsing.
	// Inline delimiters (=rubric=, *plain*) are handled by the inline parsers.
	// Any &{...} directives before the | are preserved for the inline parser.
	node.SetLines(stripPrefix(lines, prefixOffset))
	node.SetAttributeString(attrCentered, true)
}

func (t *prefixParagraphTransformer) transformDropCap(node *ast.Paragraph, lines *text.Segments, prefixOffset int) {
	// Strip ^ prefix from first line, keep as ast.Paragraph for inline parsing
	node.SetLines(stripPrefix(lines, prefixOffset))
	node.SetAttributeString(attrDropCap, true)
}

func (t *prefixParagraphTransformer) transformIndented(node *ast.Paragraph, lines *text.Segments, prefixOffset int) {
	// Strip + prefix from first line, keep as ast.Paragraph for inline parsing
	node.SetLines(stripPrefix(lines, prefixOffset))
	node.SetAttributeString(attrIndented, true)
}

func (t *prefixParagraphTransformer) transformRunningHeader(node *ast.Paragraph, reader text.Reader, lines *text.Segments, prefixOffset int) {
	stripped := stripPrefix(lines, prefixOffset)
	var sb strings.Builder
	for i := 0; i < stripped.Len(); i++ {
		seg := stripped.At(i)
		sb.Write(seg.Value(reader.Source()))
	}
	headerText := strings.TrimSpace(strings.ReplaceAll(sb.String(), "\n", " "))
	newBlock := &runningHeaderBlock{HeaderText: headerText}
	parent := node.Parent()
	parent.ReplaceChild(parent, node, newBlock)
}

// ---- Inline parser for [^id] footnote references ----

type footnoteRefInlineParser struct{}

func (p *footnoteRefInlineParser) Trigger() []byte { return []byte{'['} }
func (p *footnoteRefInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 4 || line[0] != '[' || line[1] != '^' {
		return nil // not [^ — let goldmark's link parser handle it
	}
	end := bytes.IndexByte(line[2:], ']')
	if end < 1 {
		return nil // need at least one char for ID
	}
	id := string(line[2 : 2+end])
	block.Advance(2 + end + 1) // consume [^id]
	return &footnoteRefInline{ID: id}
}

// ---- Paragraph transformer for [^id]: footnote definitions ----

type footnoteDefParagraphTransformer struct{}

func (t *footnoteDefParagraphTransformer) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	lines := node.Lines()
	if lines.Len() == 0 {
		return
	}

	firstSeg := lines.At(0)
	firstLine := firstSeg.Value(reader.Source())

	id, prefixLen := parseFootnoteDefPrefix(firstLine)
	if id == "" {
		return
	}

	// Strip [^id]: prefix from first line, keep remaining content.
	// Inline parsing will run on the remaining text normally.
	newSegs := text.NewSegments()
	newSegs.Append(firstSeg.WithStart(firstSeg.Start + prefixLen))
	for i := 1; i < lines.Len(); i++ {
		newSegs.Append(lines.At(i))
	}
	node.SetLines(newSegs)
	node.SetAttributeString(attrFootnoteDef, id)
}

// parseFootnoteDefPrefix parses a [^id]: prefix from a line.
// Returns the ID and the byte length of the prefix (including trailing space).
// Returns ("", 0) if the line doesn't match.
func parseFootnoteDefPrefix(line []byte) (string, int) {
	if len(line) < 5 || line[0] != '[' || line[1] != '^' {
		return "", 0
	}
	end := bytes.IndexByte(line[2:], ']')
	if end < 1 {
		return "", 0 // need at least one char for ID
	}
	id := string(line[2 : 2+end])

	// Expect : after ]
	afterBracket := 2 + end + 1
	if afterBracket >= len(line) || line[afterBracket] != ':' {
		return "", 0
	}
	afterColon := afterBracket + 1

	// Skip optional space after colon
	if afterColon < len(line) && line[afterColon] == ' ' {
		afterColon++
	}
	return id, afterColon
}

// skipDirectives returns the byte offset past any leading &{...} directives
// in line. If no directives are present, returns 0.
func skipDirectives(line []byte) int {
	i := 0
	for i+1 < len(line) && line[i] == '&' && line[i+1] == '{' {
		end := bytes.IndexByte(line[i+2:], '}')
		if end < 0 {
			break
		}
		i += 2 + end + 1
	}
	return i
}

// stripPrefix removes the prefix byte at the given offset from the first line
// of a segments list, preserving any &{...} directives before it.
// If offset is 0, simply advances the start by 1 (no directives to preserve).
// If offset > 0, splits the first line into two segments around the prefix byte.
func stripPrefix(lines *text.Segments, prefixOffset int) *text.Segments {
	newSegs := text.NewSegments()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if i == 0 {
			if prefixOffset == 0 {
				newSegs.Append(seg.WithStart(seg.Start + 1))
			} else {
				newSegs.Append(seg.WithStop(seg.Start + prefixOffset))
				newSegs.Append(seg.WithStart(seg.Start + prefixOffset + 1))
			}
		} else {
			newSegs.Append(seg)
		}
	}
	return newSegs
}
