package markup

import (
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"github.com/grisha/slavonic-render/ir"
)

// Parse parses Church Slavonic markdown source into an intermediate
// ir.Document. It configures goldmark with the Church Slavonic extension,
// parses the source into an AST, then walks the AST to produce the IR.
func Parse(source string) (*ir.Document, error) {
	md := goldmark.New(
		goldmark.WithExtensions(&churchSlavonicExtension{}),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	)

	src := []byte(source)
	astDoc := md.Parser().Parse(text.NewReader(src))

	w := &walker{}
	doc, err := w.walk(astDoc, src)
	if err != nil {
		return nil, fmt.Errorf("Parse: %w", err)
	}
	return doc, nil
}
