package markup

import "github.com/yuin/goldmark/ast"

// ---- Inline node types ----

// rubricInline represents inline rubric text (=text=).
type rubricInline struct {
	ast.BaseInline
	incipit bool // made by ~X (the first grapheme), not by =...=
}

var kindRubricInline = ast.NewNodeKind("rubricInline")

func (n *rubricInline) Kind() ast.NodeKind { return kindRubricInline }
func (n *rubricInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// plainTextInline represents inline plain/instruction text (*text*).
// Rendered in modern typeface (e.g. Carlito), smaller, in rubric color.
type plainTextInline struct {
	ast.BaseInline
}

var kindPlainTextInline = ast.NewNodeKind("plainTextInline")

func (n *plainTextInline) Kind() ast.NodeKind { return kindPlainTextInline }
func (n *plainTextInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// indentMarker represents an inline indent (-), rendered as horizontal space.
type indentMarker struct {
	ast.BaseInline
}

var kindIndentMarker = ast.NewNodeKind("indentMarker")

func (n *indentMarker) Kind() ast.NodeKind { return kindIndentMarker }
func (n *indentMarker) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// expressionInline represents a ${...} inline expression.
// During rendering, the Expr is evaluated; if the result is available,
// it replaces the node with text. If not, the containing paragraph is deferred.
type expressionInline struct {
	ast.BaseInline
	Expr string // the expression text, e.g. "page(c1)"
}

var kindExpressionInline = ast.NewNodeKind("expressionInline")

func (n *expressionInline) Kind() ast.NodeKind { return kindExpressionInline }
func (n *expressionInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// pageContextInline is an inline directive &{op key [value]}: a push to
// the per-page context, an anchor, a margin note or a leader.
type pageContextInline struct {
	ast.BaseInline
	Op    string // "push", "anchor", "margin", "leader"
	Key   string
	Value string // empty when the directive has two parts (&{anchor c1})
}

var kindPageContextInline = ast.NewNodeKind("pageContextInline")

func (n *pageContextInline) Kind() ast.NodeKind { return kindPageContextInline }
func (n *pageContextInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// footnoteRefInline represents an inline footnote reference [^id].
// The ID is a matching key that links to the corresponding footnote definition.
// Display numbers are assigned during the IR walk based on first-reference order.
type footnoteRefInline struct {
	ast.BaseInline
	ID string // footnote identifier, e.g. "1", "note1"
}

var kindFootnoteRef = ast.NewNodeKind("footnoteRef")

func (n *footnoteRefInline) Kind() ast.NodeKind { return kindFootnoteRef }
func (n *footnoteRefInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// ---- Block node types ----

// runningHeaderBlock is a directive that changes the running header.
// It produces no visual output.
type runningHeaderBlock struct {
	ast.BaseBlock
	HeaderText string
}

var kindRunningHeaderBlock = ast.NewNodeKind("runningHeaderBlock")

func (n *runningHeaderBlock) Kind() ast.NodeKind { return kindRunningHeaderBlock }
func (n *runningHeaderBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// pageBreakBlock is a directive that forces a new page.
type pageBreakBlock struct {
	ast.BaseBlock
}

var kindPageBreak = ast.NewNodeKind("PageBreak")

func (n *pageBreakBlock) Kind() ast.NodeKind { return kindPageBreak }
func (n *pageBreakBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}
