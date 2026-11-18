package pdfrender

import (
	"fmt"
	"unicode"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/boxesandglue/boxesandglue/frontend"
	"github.com/rivo/uniseg"

	"github.com/grisha/slavonic-render/cu"
	"github.com/grisha/slavonic-render/ir"
)

// dropCapResult holds the rendered drop cap and body text with positioning info.
type dropCapResult struct {
	body          *node.VList
	dropCap       *node.VList
	dropCapOffset bag.ScaledPoint // Vertical offset for drop cap placement
	letter        string          // The extracted drop cap letter(s)
}

// splitSpansAtGrapheme extracts the first `count` grapheme clusters from spans,
// returning the letter as a plain string and the remaining body spans with
// formatting preserved. Indent-only spans are skipped during counting but
// passed through to body.
func splitSpansAtGrapheme(spans []ir.Span, count int) (letter string, body []ir.Span) {
	remaining := count
	for i, span := range spans {
		// Pass indent-only spans through to body unchanged
		if span.Indent {
			body = append(body, span)
			continue
		}
		if span.Text == "" {
			continue
		}

		if remaining <= 0 {
			// Already extracted enough graphemes; pass rest to body
			body = append(body, spans[i:]...)
			return
		}

		// Count graphemes in this span
		gr := uniseg.NewGraphemes(span.Text)
		var consumed int  // bytes consumed
		var extracted int // graphemes extracted
		for extracted < remaining && gr.Next() {
			consumed += len(gr.Str())
			extracted++
			letter += gr.Str()
		}
		remaining -= extracted

		// If there's leftover text in this span, add it as the first body span
		if consumed < len(span.Text) {
			rest := ir.Span{
				Text:   span.Text[consumed:],
				Rubric: span.Rubric,
				Plain:  span.Plain,
				Break:  span.Break,
				Link:   span.Link,
			}
			body = append(body, rest)
			// Append all subsequent spans
			body = append(body, spans[i+1:]...)
			return
		}
		// Entire span was consumed; continue to next
	}
	return
}

// normalizeFirstSpanDigraphs applies digraph normalization to the first
// text-bearing span. Returns a new slice (does not mutate input).
func normalizeFirstSpanDigraphs(spans []ir.Span) []ir.Span {
	result := make([]ir.Span, len(spans))
	copy(result, spans)
	for i := range result {
		if result[i].Text != "" {
			result[i].Text = normalizeDropCapDigraphs(result[i].Text)
			break
		}
	}
	return result
}

// dropCapParagraph renders a paragraph with a drop cap initial letter.
func (r *renderer) dropCapParagraph(spans []ir.Span, overrides ...any) (*dropCapResult, error) {
	dcfg := r.DropCap
	rcfg := r.renderConfig

	for _, o := range overrides {
		switch v := o.(type) {
		case DropCapConfig:
			dcfg = mergeDropCapConfig(dcfg, v)
		case renderConfig:
			rcfg = mergeRenderConfig(rcfg, v)
		}
	}

	// Normalize Church Slavonic digraphs for drop cap rendering.
	spans = normalizeFirstSpanDigraphs(spans)

	// Extract drop cap letter (always one grapheme cluster)
	letter, bodySpans := splitSpansAtGrapheme(spans, 1)

	// Measure body text x-height
	var xHeight bag.ScaledPoint
	var bodyUpem float64
	if r.bodyFace != nil {
		bodyUpem = float64(r.bodyFace.OTFace().Upem())
		if gid, ok := getGlyphIDForRune(r.bodyFace, 'а'); ok {
			if ext, ok := getGlyphExtents(r.bodyFace, gid); ok {
				inkHeightRatio := float64(ext.yMax-ext.yMin) / bodyUpem
				xHeight = bag.ScaledPoint(float64(rcfg.bodySize) * inkHeightRatio)
			}
		}
	}
	if xHeight == 0 {
		xHeight = bag.ScaledPoint(float64(rcfg.bodySize) * 0.45)
	}

	// Get ink bounds for drop cap letter
	var extents *glyphExtents
	var dropCapUpem float64
	if r.dropCapFace != nil {
		dropCapUpem = float64(r.dropCapFace.OTFace().Upem())
		firstRune := []rune(letter)[0]
		if gid, ok := getGlyphIDForRune(r.dropCapFace, firstRune); ok {
			if ext, ok := getGlyphExtents(r.dropCapFace, gid); ok {
				extents = &ext
			}
		}
	}

	// Compute drop cap font size from glyph ink extents
	var dropCapFontSize bag.ScaledPoint
	if extents != nil {
		dropCapInkRatio := float64(extents.yMax-extents.yMin) / dropCapUpem
		dropCapFontSize = computeDropCapFontSize(xHeight, rcfg.leading, dcfg.Lines, dcfg.Overhang, dropCapInkRatio)
	} else {
		// Glyph not in drop cap font cmap — fall back to a reasonable ratio
		dropCapFontSize = computeDropCapFontSize(xHeight, rcfg.leading, dcfg.Lines, dcfg.Overhang, 0.54)
	}

	// Format drop cap text node
	dropCapPara := &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: r.dropCapFont,
			frontend.SettingSize:       dropCapFontSize,
			frontend.SettingColor:      cu.Kinovar,
		},
		Items: []any{letter},
	}

	// Calculate indent width
	var indent bag.ScaledPoint
	if extents != nil {
		scale := dropCapFontSize.ToPT() / dropCapUpem
		var inkWidth float64
		if extents.xMin > 0 {
			inkWidth = float64(extents.xMax) * scale
		} else {
			inkWidth = float64(extents.xMax-extents.xMin) * scale
		}
		indent = bag.ScaledPoint(inkWidth*65536) + dcfg.Margin
	} else {
		// Fallback: measure advance width
		dropCapVL, _, err := r.doc.FormatParagraph(dropCapPara, bag.MustSP("100pt"),
			frontend.Leading(dropCapFontSize),
			frontend.HorizontalAlign(frontend.HAlignLeft),
		)
		if err != nil {
			return nil, fmt.Errorf("DropCapParagraph: measuring drop cap: %w", err)
		}

		var measuredWidth bag.ScaledPoint
		for vn := dropCapVL.List; vn != nil; vn = vn.Next() {
			if hlist, ok := vn.(*node.HList); ok {
				for hn := hlist.List; hn != nil; hn = hn.Next() {
					if glyph, ok := hn.(*node.Glyph); ok {
						measuredWidth += glyph.Width
					} else if kern, ok := hn.(*node.Kern); ok {
						measuredWidth += kern.Kern
					}
				}
				break
			}
		}
		indent = measuredWidth + dcfg.Margin
	}

	// Format drop cap with generous width
	dropCapVL, _, err := r.doc.FormatParagraph(dropCapPara, bag.MustSP("100pt"),
		frontend.Leading(dropCapFontSize),
		frontend.HorizontalAlign(frontend.HAlignLeft),
	)
	if err != nil {
		return nil, fmt.Errorf("DropCapParagraph: formatting drop cap: %w", err)
	}

	// Format body text with indent
	bodyPara := ir.Paragraph{Content: bodySpans}
	bodyText := paragraphToText(&bodyPara, r.textConfig())
	bodyText.Settings[frontend.SettingSize] = rcfg.bodySize

	opts := []frontend.TypesettingOption{
		frontend.Leading(rcfg.leading),
		frontend.IndentLeft(indent, dcfg.Lines),
	}
	if r.language != nil {
		opts = append(opts, frontend.Language(r.language))
	}
	if rcfg.hyphenPenalty != 0 {
		opts = append(opts, frontend.HyphenPenalty(rcfg.hyphenPenalty))
	}

	bodyVL, _, err := r.doc.FormatParagraph(bodyText, rcfg.textWidth, opts...)
	if err != nil {
		return nil, fmt.Errorf("DropCapParagraph: formatting body: %w", err)
	}
	alignLeaders(bodyVL)

	// Compute vertical positioning
	var dropCapHListHeight, bodyHListHeight bag.ScaledPoint
	for n := dropCapVL.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			dropCapHListHeight = hlist.Height
			break
		}
	}
	for n := bodyVL.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			bodyHListHeight = hlist.Height
			break
		}
	}

	var dropCapOffset bag.ScaledPoint
	if extents != nil {
		scale := dropCapFontSize.ToPT() / dropCapUpem
		inkBottomSP := bag.ScaledPoint(float64(extents.yMin) * scale * 65536)
		dropCapOffset = dropCapHListHeight - inkBottomSP - bodyHListHeight - bag.ScaledPoint(dcfg.Lines-1)*rcfg.leading
	} else {
		dropCapOffset = dropCapHListHeight - bodyHListHeight - bag.ScaledPoint(dcfg.Lines-1)*rcfg.leading
	}

	return &dropCapResult{
		body:          bodyVL,
		dropCap:       dropCapVL,
		dropCapOffset: dropCapOffset,
		letter:        letter,
	}, nil
}

// normalizeDropCapDigraphs replaces Church Slavonic digraphs at the start of
// text with their monograph equivalents for drop cap font rendering.
//
// The "uk" digraph (О+у or ᲂ+у, representing the "u" sound) is a single
// letter written as two characters. For drop cap rendering, it must be
// replaced with the monograph Ꙋ (U+A64A) so that:
//  1. splitSpansAtGrapheme extracts it as one unit (not just the О/ᲂ)
//  2. The drop cap font (Indiction) renders the correct decorative glyph
func normalizeDropCapDigraphs(text string) string {
	runes := []rune(text)
	if len(runes) < 2 {
		return text
	}
	// Check for О (U+041E) or ᲂ (U+1C82) followed by у (U+0443)
	if (runes[0] == '\u041E' || runes[0] == '\u1C82') && runes[1] == '\u0443' {
		// Skip any combining marks after у
		i := 2
		for i < len(runes) && unicode.Is(unicode.Mn, runes[i]) {
			i++
		}
		return "\uA64A" + string(runes[i:])
	}
	return text
}

// computeDropCapFontSize calculates font size for a drop cap to span N lines.
func computeDropCapFontSize(xHeight, leading bag.ScaledPoint, lines int, overhang, dropCapInkRatio float64) bag.ScaledPoint {
	textSpan := float64(lines)*xHeight.ToPT() + float64(lines-1)*leading.ToPT()
	targetInkHeight := textSpan * overhang
	fontSize := targetInkHeight / dropCapInkRatio
	return bag.MustSP(fmt.Sprintf("%.3fpt", fontSize))
}
