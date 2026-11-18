// Package pdfrender sets a parsed document (ir.Document) as a PDF, with
// boxesandglue: NewDocumentFromConfig makes a DocumentRenderer, which does it.
package pdfrender

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"unicode"

	pdf "github.com/boxesandglue/baseline-pdf"
	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/lang"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/boxesandglue/boxesandglue/frontend"
	"github.com/rivo/uniseg"

	"github.com/grisha/slavonic-render/cu"
	"github.com/grisha/slavonic-render/internal/assets"
	"github.com/grisha/slavonic-render/ir"
)

// DropCapConfig is the drop cap's proportions.
type DropCapConfig struct {
	Lines    int             // the lines of text it spans
	Margin   bag.ScaledPoint // the gap between it and the text; 0: in proportion to the body size
	Overhang float64         // its height as a multiple of the lines it spans
}

// renderConfig holds text rendering parameters.
type renderConfig struct {
	bodySize      bag.ScaledPoint // Font size for body text
	leading       bag.ScaledPoint // Line spacing (baseline to baseline)
	textWidth     bag.ScaledPoint // Available width for text
	hyphenPenalty int             // higher, fewer hyphens; 0: boxesandglue's own
}

type fontFamilies struct {
	bodyFont    *frontend.FontFamily
	dropCapFont *frontend.FontFamily
	plainFont   *frontend.FontFamily // Modern typeface (e.g. Carlito) for instruction text
}

// Config is the typesetting of the text: its size, its leading, how readily
// it is hyphenated, its drop caps. A Leading or a DropCap.Margin of 0 is set
// in proportion to BodySize. The fonts and the hyphenation patterns are
// loaded by NewDocumentFromConfig.
type Config struct {
	*fontFamilies

	BodySize bag.ScaledPoint
	Leading  bag.ScaledPoint // baseline to baseline

	language      *lang.Lang // the hyphenation patterns (nil: none)
	HyphenPenalty int        // higher, fewer hyphens; 0: boxesandglue's own

	DropCap DropCapConfig
}

// renderer provides a high-level API for typesetting Church Slavonic text.
type renderer struct {
	doc *frontend.Document
	Config

	dropCapFace *pdf.Face
	bodyFace    *pdf.Face

	margin       bag.ScaledPoint // page margin, set by pageBuilder
	renderConfig renderConfig

	// The ornaments loaded into the document, by name: each is loaded
	// once, so an ornament placed again is the same object of the PDF.
	ornaments map[string]*pdf.Imagefile
}

// setTextWidth updates the renderer's text width. Called by pageBuilder
// so that the margin is the single source of truth for layout.
func (r *renderer) setTextWidth(w bag.ScaledPoint) {
	r.renderConfig.textWidth = w
}

// setMargin tells the renderer the page margin, used to size margin notes.
func (r *renderer) setMargin(m bag.ScaledPoint) {
	r.margin = m
}

// lead is the fraction f of the body leading, the unit of every vertical
// distance that belongs to the type (the spaces between blocks, the leading
// of headings and of one-line elements), so that it scales with the body
// size. Rounded, so that a fraction that is a whole number of points is one.
func (r *renderer) lead(f float64) bag.ScaledPoint {
	return bag.ScaledPoint(math.Round(float64(r.Leading) * f))
}

// newRenderer creates a new renderer with the given configuration. The
// leading and the drop cap's gap follow the body size unless they are set.
func newRenderer(doc *frontend.Document, cfg Config) *renderer {
	if cfg.Leading == 0 {
		cfg.Leading = leadingFor(cfg.BodySize)
	}
	if cfg.DropCap.Margin == 0 {
		cfg.DropCap.Margin = dropCapMarginFor(cfg.BodySize)
	}
	r := &renderer{
		doc:    doc,
		Config: cfg,
		renderConfig: renderConfig{
			bodySize:      cfg.BodySize,
			leading:       cfg.Leading,
			hyphenPenalty: cfg.HyphenPenalty,
		},
		ornaments: map[string]*pdf.Imagefile{},
	}

	// Load font faces for glyph extent queries
	if fs, err := cfg.dropCapFont.GetFontSource(frontend.FontWeight400, frontend.FontStyleNormal); err == nil {
		if face, err := doc.LoadFace(fs); err == nil {
			r.dropCapFace = face
		}
	}
	if fs, err := cfg.bodyFont.GetFontSource(frontend.FontWeight400, frontend.FontStyleNormal); err == nil {
		if face, err := doc.LoadFace(fs); err == nil {
			r.bodyFace = face
		}
	}

	return r
}

// renderParagraph renders a ir.Paragraph to a VList. It calls ToText() to
// convert spans into a frontend.Text node, then formats it with alignment
// based on p.Centered (centered vs justified).
func (r *renderer) renderParagraph(p ir.Paragraph, overrides ...any) (*node.VList, error) {
	rcfg := r.renderConfig
	for _, o := range overrides {
		if v, ok := o.(renderConfig); ok {
			rcfg = mergeRenderConfig(rcfg, v)
		}
	}

	if p.Minor {
		rcfg.bodySize = bag.ScaledPoint(float64(rcfg.bodySize) * minorScale)
		rcfg.leading = bag.ScaledPoint(float64(rcfg.leading) * minorScale)
	}

	cfg := r.textConfig()
	text := paragraphToText(&p, cfg)
	text.Settings[frontend.SettingSize] = rcfg.bodySize

	// A paragraph with no text at its own size (all rubric, all plain) is set
	// on the leading of its largest text, in the paragraph's proportion.
	if largest := largestSize(&p, rcfg.bodySize, cfg); largest > 0 && largest < rcfg.bodySize {
		rcfg.leading = bag.ScaledPoint(math.Round(float64(rcfg.leading) * float64(largest) / float64(rcfg.bodySize)))
	}

	if p.Indented {
		text.Settings[frontend.SettingIndentLeft] = r.BodySize
		text.Settings[frontend.SettingIndentLeftRows] = 0 // all rows
	}

	align := frontend.HAlignJustified
	if p.Centered {
		align = frontend.HAlignCenter
	}

	opts := []frontend.TypesettingOption{
		frontend.Leading(rcfg.leading),
		frontend.HorizontalAlign(align),
	}
	if r.language != nil {
		opts = append(opts, frontend.Language(r.language))
	}
	if rcfg.hyphenPenalty != 0 {
		opts = append(opts, frontend.HyphenPenalty(rcfg.hyphenPenalty))
	}

	vlist, _, err := r.doc.FormatParagraph(text, rcfg.textWidth, opts...)
	if err != nil {
		return nil, fmt.Errorf("RenderParagraph: %w", err)
	}
	alignLeaders(vlist)

	return vlist, nil
}

// title renders a large centered title in rubric color.
func (r *renderer) title(text string, overrides ...any) (*node.VList, error) {
	rcfg := r.renderConfig
	for _, o := range overrides {
		if v, ok := o.(renderConfig); ok {
			rcfg = mergeRenderConfig(rcfg, v)
		}
	}

	titleSize := bag.ScaledPoint(float64(rcfg.bodySize) * titleScale)

	para := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: r.bodyFont,
			frontend.SettingSize:       titleSize,
			frontend.SettingColor:      cu.Kinovar,
		},
		Items: []any{text},
	}

	opts := []frontend.TypesettingOption{
		frontend.Leading(titleSize + r.lead(displayLead)),
		frontend.HorizontalAlign(frontend.HAlignCenter),
	}
	if r.language != nil {
		opts = append(opts, frontend.Language(r.language))
	}
	if rcfg.hyphenPenalty != 0 {
		opts = append(opts, frontend.HyphenPenalty(rcfg.hyphenPenalty))
	}

	vlist, _, err := r.doc.FormatParagraph(para, rcfg.textWidth, opts...)
	if err != nil {
		return nil, fmt.Errorf("Title: %w", err)
	}

	return vlist, nil
}

// header renders a running header (smaller, centered, red, lowercased).
func (r *renderer) header(text string, overrides ...any) (*node.VList, error) {
	rcfg := r.renderConfig
	for _, o := range overrides {
		if v, ok := o.(renderConfig); ok {
			rcfg = mergeRenderConfig(rcfg, v)
		}
	}

	headerText := lowercaseFirst(text)
	headerSize := bag.ScaledPoint(float64(rcfg.bodySize) * furnitureScale)

	para := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: r.bodyFont,
			frontend.SettingSize:       headerSize,
			frontend.SettingColor:      cu.Kinovar,
		},
		Items: []any{headerText},
	}

	opts := []frontend.TypesettingOption{
		frontend.Leading(headerSize + r.lead(lineLead)),
		frontend.HorizontalAlign(frontend.HAlignCenter),
	}
	if r.language != nil {
		opts = append(opts, frontend.Language(r.language))
	}

	vlist, _, err := r.doc.FormatParagraph(para, rcfg.textWidth, opts...)
	if err != nil {
		return nil, fmt.Errorf("Header: %w", err)
	}

	return vlist, nil
}

// pageNumber renders a page number for corner placement.
func (r *renderer) pageNumber(numText string, overrides ...any) (*node.VList, error) {
	rcfg := r.renderConfig
	for _, o := range overrides {
		if v, ok := o.(renderConfig); ok {
			rcfg = mergeRenderConfig(rcfg, v)
		}
	}

	pageNumSize := bag.ScaledPoint(float64(rcfg.bodySize) * furnitureScale)

	para := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: r.bodyFont,
			frontend.SettingSize:       pageNumSize,
			frontend.SettingColor:      cu.Black,
		},
		Items: []any{numText},
	}

	vlist, _, err := r.doc.FormatParagraph(para, rcfg.textWidth,
		frontend.Leading(pageNumSize+r.lead(lineLead)),
		frontend.HorizontalAlign(frontend.HAlignLeft),
	)
	if err != nil {
		return nil, fmt.Errorf("PageNumber: %w", err)
	}

	// Measure actual width and set it on VList
	var actualWidth bag.ScaledPoint
	for n := vlist.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			for hn := hlist.List; hn != nil; hn = hn.Next() {
				if glyph, ok := hn.(*node.Glyph); ok {
					actualWidth += glyph.Width
				} else if kern, ok := hn.(*node.Kern); ok {
					actualWidth += kern.Kern
				}
			}
			break
		}
	}
	vlist.Width = actualWidth

	return vlist, nil
}

// catchwordColor overrides the catchword text color (e.g. red for headings).
type catchwordColor struct {
	color string
}

// catchword renders a catchword (кустода) - the first word of the next page.
func (r *renderer) catchword(text string, redFirstLetter bool, overrides ...any) (*node.VList, error) {
	rcfg := r.renderConfig
	var colorOverride string
	for _, o := range overrides {
		if v, ok := o.(renderConfig); ok {
			rcfg = mergeRenderConfig(rcfg, v)
		}
		if v, ok := o.(catchwordColor); ok {
			colorOverride = v.color
		}
	}

	catchwordSize := bag.ScaledPoint(float64(rcfg.bodySize) * furnitureScale)

	var para *frontend.Text
	if colorOverride != "" {
		para = &frontend.Text{
			Settings: frontend.TypesettingSettings{
				frontend.SettingFontFamily: r.bodyFont,
				frontend.SettingSize:       catchwordSize,
				frontend.SettingColor:      colorOverride,
			},
			Items: []any{text},
		}
	} else if redFirstLetter {
		firstLetter, bodySpans := splitSpansAtGrapheme([]ir.Span{{Text: text}}, 1)
		rest := bodySpans[0].Text
		para = &frontend.Text{
			Settings: frontend.TypesettingSettings{
				frontend.SettingFontFamily: r.bodyFont,
				frontend.SettingSize:       catchwordSize,
			},
			Items: []any{
				&frontend.Text{
					Settings: frontend.TypesettingSettings{
						frontend.SettingColor: cu.Kinovar,
					},
					Items: []any{firstLetter},
				},
				&frontend.Text{
					Settings: frontend.TypesettingSettings{
						frontend.SettingColor: cu.Black,
					},
					Items: []any{rest},
				},
			},
		}
	} else {
		para = &frontend.Text{
			Settings: frontend.TypesettingSettings{
				frontend.SettingFontFamily: r.bodyFont,
				frontend.SettingSize:       catchwordSize,
				frontend.SettingColor:      cu.Black,
			},
			Items: []any{text},
		}
	}

	vlist, _, err := r.doc.FormatParagraph(para, rcfg.textWidth,
		frontend.Leading(catchwordSize+r.lead(lineLead)),
		frontend.HorizontalAlign(frontend.HAlignLeft),
	)
	if err != nil {
		return nil, fmt.Errorf("Catchword: %w", err)
	}

	// Measure actual width
	var actualWidth bag.ScaledPoint
	for n := vlist.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			for hn := hlist.List; hn != nil; hn = hn.Next() {
				if glyph, ok := hn.(*node.Glyph); ok {
					actualWidth += glyph.Width
				} else if kern, ok := hn.(*node.Kern); ok {
					actualWidth += kern.Kern
				}
			}
			break
		}
	}
	vlist.Width = actualWidth

	return vlist, nil
}

// marginNote renders a small rubric-colored text node for placing in the left margin.
// The text is formatted right-aligned within the page margin (minus a small gap),
// with no hyphenation.
func (r *renderer) marginNote(text string) (*node.VList, error) {
	noteSize := bag.ScaledPoint(float64(r.BodySize) * marginNoteScale)
	marginWidth := r.margin - marginNoteGap
	if marginWidth <= 0 {
		marginWidth = bag.MustSP("10mm") // fallback
	}

	para := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: r.bodyFont,
			frontend.SettingSize:       noteSize,
			frontend.SettingColor:      cu.Kinovar,
		},
		Items: []any{text},
	}

	vlist, _, err := r.doc.FormatParagraph(para, marginWidth,
		frontend.Leading(noteSize+r.lead(lineLead)),
		frontend.HorizontalAlign(frontend.HAlignRight),
		frontend.HyphenPenalty(10000), // disable hyphenation
	)
	if err != nil {
		return nil, fmt.Errorf("MarginNote: %w", err)
	}

	return vlist, nil
}

// renderFootnote renders a footnote body with its number prefix.
// The content uses the plain (modern) font in italic, smaller than the body,
// with rubric color. Rubric spans within the content switch to the CU body font.
func (r *renderer) renderFootnote(num int, content []ir.Span) (*node.VList, error) {
	footnoteSize := bag.ScaledPoint(float64(r.BodySize) * footnoteScale)
	footnoteLeading := bag.ScaledPoint(float64(r.Leading) * footnoteScale)

	// Mark all non-rubric content spans as Plain so they render in red.
	bodySpans := make([]ir.Span, len(content))
	for i, s := range content {
		if !s.Rubric {
			s.Plain = true
		}
		bodySpans[i] = s
	}

	font := r.plainFont
	if font == nil {
		font = r.bodyFont
	}

	cfg := textConfig{
		bodyFont:   font,
		bodySize:   footnoteSize,
		rubricFont: r.bodyFont, // =rubric= inside footnotes uses CU font
		plainFont:  font,
		plainSize:  footnoteSize,
	}

	// Build the number prefix as a bold Text node, followed by body content.
	numPrefix := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingColor:      cu.Kinovar,
			frontend.SettingFontWeight: frontend.FontWeight700,
		},
		Items: []any{fmt.Sprintf("%d. ", num)},
	}

	bodyPara := &ir.Paragraph{Content: bodySpans}
	bodyText := paragraphToText(bodyPara, cfg)

	text := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: font,
			frontend.SettingSize:       footnoteSize,
			frontend.SettingStyle:      frontend.FontStyleItalic,
			frontend.SettingColor:      cu.Kinovar,
		},
		Items: []any{numPrefix, bodyText},
	}

	vlist, _, err := r.doc.FormatParagraph(text, r.renderConfig.textWidth,
		frontend.Leading(footnoteLeading),
		frontend.HorizontalAlign(frontend.HAlignJustified),
	)
	if err != nil {
		return nil, fmt.Errorf("RenderFootnote: %w", err)
	}
	alignLeaders(vlist)

	return vlist, nil
}

// footnoteRule renders a horizontal separator rule for the footnote area.
// Returns the VList and a small gap height to place above it.
func (r *renderer) footnoteRule() (*node.VList, error) {
	textWidth := r.renderConfig.textWidth
	ruleWidth := bag.ScaledPoint(float64(textWidth) / footnoteRuleDivisor)
	ruleHeight := bag.MustSP("0.4pt")

	rule := node.NewRule()
	rule.Width = ruleWidth
	rule.Height = ruleHeight

	hlist := node.NewHList()
	hlist.List = rule
	hlist.Width = ruleWidth
	hlist.Height = ruleHeight

	vlist := node.NewVList()
	vlist.List = hlist
	vlist.Width = ruleWidth
	vlist.Height = ruleHeight

	return vlist, nil
}

// ornamentConfig configures ornamental band rendering.
type ornamentConfig struct {
	width   bag.ScaledPoint // Target width (default: text width)
	widthMM int             // Target width in mm (alternative to Width; takes precedence if non-zero)
	height  bag.ScaledPoint // Override aspect ratio
	center  bool            // Center horizontally (default: true)
}

// ornament returns the ornament of the given name (assets.PDF) as a VList.
func (r *renderer) ornament(name string, overrides ...any) (*node.VList, error) {
	cfg := ornamentConfig{
		width:  r.renderConfig.textWidth,
		center: true,
	}

	for _, o := range overrides {
		if v, ok := o.(ornamentConfig); ok {
			if v.widthMM > 0 {
				cfg.width = bag.MustSP(fmt.Sprintf("%dmm", v.widthMM))
			} else if v.width != 0 {
				cfg.width = v.width
			}
			if v.height != 0 {
				cfg.height = v.height
			}
			cfg.center = v.center
		}
	}

	imgFile, ok := r.ornaments[name]
	if !ok {
		data, err := assets.PDF(name)
		if err != nil {
			return nil, fmt.Errorf("Ornament: %w", err)
		}
		imgFile, err = r.doc.Doc.LoadImageFromReader(bytes.NewReader(data), "/MediaBox", 1)
		if err != nil {
			return nil, fmt.Errorf("Ornament: loading %q: %w", name, err)
		}
		r.ornaments[name] = imgFile
	}

	imgNode := r.doc.Doc.CreateImageNodeFromImagefile(imgFile, 1, "/MediaBox")
	if imgNode == nil {
		return nil, fmt.Errorf("Ornament: creating image node from %q", name)
	}

	targetWidth := cfg.width
	var targetHeight bag.ScaledPoint
	if cfg.height != 0 {
		targetHeight = cfg.height
	} else {
		aspectRatio := float64(imgNode.Height) / float64(imgNode.Width)
		targetHeight = bag.ScaledPoint(float64(targetWidth) * aspectRatio)
	}

	imgNode.Width = targetWidth
	imgNode.Height = targetHeight

	hlist := node.NewHList()
	hlist.List = imgNode
	hlist.Width = targetWidth
	hlist.Height = targetHeight

	vlist := node.NewVList()
	vlist.List = hlist
	vlist.Width = targetWidth
	vlist.Height = targetHeight

	return vlist, nil
}

// sectionHeading renders a section heading (smaller than the title).
// Used for ## headings in the markup.
func (r *renderer) sectionHeading(text string, overrides ...any) (*node.VList, error) {
	rcfg := r.renderConfig
	for _, o := range overrides {
		if v, ok := o.(renderConfig); ok {
			rcfg = mergeRenderConfig(rcfg, v)
		}
	}

	headingSize := bag.ScaledPoint(float64(rcfg.bodySize) * headingScale)

	para := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: r.bodyFont,
			frontend.SettingSize:       headingSize,
			frontend.SettingColor:      cu.Kinovar,
		},
		Items: []any{text},
	}

	opts := []frontend.TypesettingOption{
		frontend.Leading(headingSize + r.lead(displayLead)),
		frontend.HorizontalAlign(frontend.HAlignCenter),
	}
	if r.language != nil {
		opts = append(opts, frontend.Language(r.language))
	}
	if rcfg.hyphenPenalty != 0 {
		opts = append(opts, frontend.HyphenPenalty(rcfg.hyphenPenalty))
	}

	vlist, _, err := r.doc.FormatParagraph(para, rcfg.textWidth, opts...)
	if err != nil {
		return nil, fmt.Errorf("SectionHeading: %w", err)
	}

	return vlist, nil
}

// Helper functions

func mergeRenderConfig(base, override renderConfig) renderConfig {
	result := base
	if override.bodySize != 0 {
		result.bodySize = override.bodySize
	}
	if override.leading != 0 {
		result.leading = override.leading
	}
	if override.textWidth != 0 {
		result.textWidth = override.textWidth
	}
	if override.hyphenPenalty != 0 {
		result.hyphenPenalty = override.hyphenPenalty
	}
	return result
}

func mergeDropCapConfig(base, override DropCapConfig) DropCapConfig {
	result := base
	if override.Lines != 0 {
		result.Lines = override.Lines
	}
	if override.Margin != 0 {
		result.Margin = override.Margin
	}
	if override.Overhang != 0 {
		result.Overhang = override.Overhang
	}
	return result
}

// countGraphemes returns the number of grapheme clusters in text.
func countGraphemes(text string) int {
	gr := uniseg.NewGraphemes(text)
	n := 0
	for gr.Next() {
		n++
	}
	return n
}

func lowercaseFirst(s string) string {
	if s == "" {
		return s
	}
	gr := uniseg.NewGraphemes(s)
	if !gr.Next() {
		return s
	}
	first := gr.Str()
	rest := s[len(first):]
	lowered := strings.Map(unicode.ToLower, first)
	return lowered + rest
}
