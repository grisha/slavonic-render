package pdfrender

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/document"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/boxesandglue/boxesandglue/frontend"

	"github.com/grisha/slavonic-render/cu"
	"github.com/grisha/slavonic-render/ir"
)

// textConfig provides colors and fonts for paragraphToText conversion.
type textConfig struct {
	bodyFont    *frontend.FontFamily
	bodySize    bag.ScaledPoint      // Body font size (needed for footnote superscript sizing)
	rubricFont  *frontend.FontFamily // If non-nil, rubric spans use this font family (e.g., CU font inside plain blocks)
	plainFont   *frontend.FontFamily // Modern font (e.g. Carlito) for *text* spans
	plainSize   bag.ScaledPoint      // Size for plain spans
	indentWidth bag.ScaledPoint      // Width of inline indent (-)
	rubricSize  bag.ScaledPoint      // Size for rubric spans other than incipits (0: the paragraph's size)
}

// textConfig returns a textConfig populated from the renderer's fonts and sizes.
func (r *renderer) textConfig() textConfig {
	return textConfig{
		bodyFont:    r.bodyFont,
		bodySize:    r.BodySize,
		plainFont:   r.plainFont,
		plainSize:   bag.ScaledPoint(float64(r.BodySize) * plainScale),
		indentWidth: r.BodySize,
		rubricSize:  bag.ScaledPoint(float64(r.BodySize) * minorScale),
	}
}

// leaderSpace surrounds the glyph a leader repeats, so that the copies are
// spaced, not touching: " . . . " rather than "......" (LaTeX's \@dotsep).
const leaderSpace = " "

// alignLeaders makes the leaders of a formatted paragraph end in one column.
// A leader takes what is left of its line, so it ends where the text after
// it begins, and that moves with the text's width: later before «д҃.» than
// before «р҃г.». Its dots stand on a grid, so the last one would fall in one
// column or the next. Each leader gives up the difference between the
// widest text after a leader in the paragraph and its own, so that the text
// stands in a column as wide as the widest. This is LaTeX's box for the
// page number (\@pnumwidth), its width measured on the lines rather than
// fixed. Hpack has already given every glue its final width, which the PDF
// writer draws, so the lines can be adjusted after they are packed.
//
// In the column, Arabic numbers are set flush right, so that units stand
// under units; anything else, Slavonic numerals among them, is centred:
// they keep no place value in a fixed position («г҃і» is 13, units first),
// and flush right they line up only by their periods. The difference goes
// as kerns before and after the text; the one after goes after its last
// glyph, inside the hyperlink, so that the line is a link to the margin.
func alignLeaders(vl *node.VList) {
	type leaderLine struct {
		head   node.Node // the line's list
		leader *node.Glue
		last   node.Node       // the last glyph after the leader (the leader if none)
		tail   bag.ScaledPoint // the width of what follows the leader
	}
	var lines []leaderLine
	var widest bag.ScaledPoint
	arabic := true // every text after a leader has a digit and no letter
	for n := vl.List; n != nil; n = n.Next() {
		hl, ok := n.(*node.HList)
		if !ok {
			continue
		}
		for e := hl.List; e != nil; e = e.Next() {
			g, ok := e.(*node.Glue)
			if !ok || g.Leader == nil {
				continue
			}
			l := leaderLine{head: hl.List, leader: g, last: g}
			l.tail, _, _ = node.Dimensions(g.Next(), nil, node.Horizontal)
			digit, letter := false, false
			for t := g.Next(); t != nil; t = t.Next() {
				if gl, ok := t.(*node.Glyph); ok {
					l.last = gl
					digit = digit || strings.IndexFunc(gl.Components, unicode.IsDigit) >= 0
					letter = letter || strings.IndexFunc(gl.Components, unicode.IsLetter) >= 0
				}
			}
			arabic = arabic && digit && !letter
			lines = append(lines, l)
			widest = max(widest, l.tail)
			break
		}
	}
	for _, l := range lines {
		// A leader shorter than the difference (a title that nearly fills
		// its line) gives up all it has.
		d := min(widest-l.tail, l.leader.Width)
		if d <= 0 {
			continue
		}
		l.leader.Width -= d
		before := d / 2
		if arabic {
			before = d
		}
		for _, k := range []struct {
			after node.Node
			kern  bag.ScaledPoint
		}{{l.leader, before}, {l.last, d - before}} {
			if k.kern > 0 {
				n := node.NewKern()
				n.Kern = k.kern
				node.InsertAfter(l.head, k.after, n)
			}
		}
	}
}

// paragraphToText converts a ir.Paragraph to a boxesandglue *frontend.Text.
// This handles rubric spans (red, and in the minor size unless they are
// incipits), forced line breaks, inline indents, leaders, and hyperlinks.
func paragraphToText(p *ir.Paragraph, cfg textConfig) *frontend.Text {
	// Simple case: black text without inline formatting
	simple := true
	for _, span := range p.Content {
		if span.Rubric || span.Plain || span.Break || span.Indent || span.Leader != "" || span.Link != "" || span.FootnoteRef > 0 {
			simple = false
		}
	}
	if simple {
		return &frontend.Text{
			Settings: frontend.TypesettingSettings{
				frontend.SettingFontFamily: cfg.bodyFont,
				frontend.SettingColor:      cu.Black,
			},
			Items: []any{p.PlainText()},
		}
	}

	// Otherwise: create nested Text nodes for each span
	items := make([]any, 0, len(p.Content))

	// Consecutive spans with the same link share one hyperlink: a pair of
	// StartStop nodes around them, injected directly rather than through
	// SettingHyperlink: boxesandglue's Mknodes (seen at v0.2.61) keeps the
	// first hyperlink open over a neighbouring text that sets another, so
	// every line of a contents would link to the first line's anchor.
	// setLink closes the open one when the link changes and opens the
	// next; "" closes it.
	var startHL *node.StartStop
	var startDest string
	setLink := func(dest string) {
		if dest == startDest {
			return
		}
		if startHL != nil {
			stopHL := node.NewStartStop()
			stopHL.StartNode = startHL
			items = append(items, stopHL)
			startHL = nil
		}
		startDest = dest
		if dest != "" {
			startHL = node.NewStartStop()
			startHL.Action = node.ActionHyperlink
			startHL.Value = &document.Hyperlink{Local: dest}
			items = append(items, startHL)
		}
	}

	for _, span := range p.Content {
		setLink(span.Link)

		if span.Indent && cfg.indentWidth > 0 {
			// An empty box, not a kern: boxesandglue strips leading glue
			// and kerns from a paragraph (CSS whitespace collapsing), which
			// silently removed the indent. A box is never collapsed; it is
			// also how TeX implements \parindent.
			box := node.NewHList()
			box.Width = cfg.indentWidth
			items = append(items, box)
		}

		// Footnote reference: superscript number
		if span.FootnoteRef > 0 {
			superSize := bag.ScaledPoint(float64(cfg.bodySize) * footnoteRefScale)
			superShift := bag.ScaledPoint(float64(cfg.bodySize) * footnoteRefRise)
			numText := fmt.Sprintf("%d", span.FootnoteRef)
			superSettings := frontend.TypesettingSettings{
				frontend.SettingSize:       superSize,
				frontend.SettingColor:      cu.Kinovar,
				frontend.SettingYOffset:    superShift,
				frontend.SettingFontWeight: frontend.FontWeight700,
			}
			if cfg.plainFont != nil {
				superSettings[frontend.SettingFontFamily] = cfg.plainFont
			}
			items = append(items, &frontend.Text{
				Settings: superSettings,
				Items:    []any{numText},
			})
		}

		if span.Text != "" || span.Leader != "" {
			spanColor := cu.Black
			if span.Rubric || span.Plain {
				spanColor = cu.Kinovar
			}

			spanSettings := frontend.TypesettingSettings{
				frontend.SettingColor: spanColor,
			}
			// Rubric text is set in the minor size; the red initial of ~X
			// keeps the paragraph's size.
			if span.Rubric && !span.Incipit && cfg.rubricSize > 0 {
				spanSettings[frontend.SettingSize] = cfg.rubricSize
			}
			// When RubricFont is set (e.g., CU font inside a plain/modern block),
			// rubric spans switch font family in addition to color.
			if span.Rubric && cfg.rubricFont != nil {
				spanSettings[frontend.SettingFontFamily] = cfg.rubricFont
			}
			// Plain spans use the modern font and smaller size.
			if span.Plain && !span.Rubric && cfg.plainFont != nil {
				spanSettings[frontend.SettingFontFamily] = cfg.plainFont
				if cfg.plainSize > 0 {
					spanSettings[frontend.SettingSize] = cfg.plainSize
				}
			}

			if span.Leader != "" {
				// A leader is glue of the highest order (filll), drawn as
				// copies of its pattern on a grid shared by the whole page,
				// so the dots of successive lines stand in columns. It takes
				// all that is left of the line: it outstretches the fill
				// glue of a forced break and the paragraph's last line. The
				// penalty makes it no breakpoint (glue is one only after a
				// box), so the text after it stays on its line, at the
				// right margin.
				noBreak := node.NewPenalty()
				noBreak.Penalty = 10000
				items = append(items, noBreak)
				spanSettings[frontend.SettingLeader] = leaderSpace + span.Leader + leaderSpace
				items = append(items, &frontend.Text{Settings: spanSettings})
				gap := node.NewKern()
				gap.Kern = bag.ScaledPoint(float64(cfg.bodySize) * leaderGap)
				items = append(items, gap)
			} else {
				items = append(items, &frontend.Text{
					Settings: spanSettings,
					Items:    []any{span.Text},
				})
			}
		}

		if span.Break {
			// A link does not run across a forced break.
			setLink("")

			// Forced line break: penalty(10000) prevents break before the
			// fill-glue, then penalty(-10000) forces break after it.
			// The fill-glue absorbs remaining line width so the line
			// isn't stretched (left-aligned to the break point).
			p1 := node.NewPenalty()
			p1.Penalty = 10000
			items = append(items, p1)

			g := node.NewGlue()
			g.Stretch = bag.Factor
			g.StretchOrder = node.StretchFill
			items = append(items, g)

			p2 := node.NewPenalty()
			p2.Penalty = -10000
			items = append(items, p2)
		}
	}
	setLink("")

	return &frontend.Text{
		Settings: frontend.TypesettingSettings{
			frontend.SettingFontFamily: cfg.bodyFont,
		},
		Items: items,
	}
}

// largestSize is the size of the largest text in p, sized as paragraphToText
// sizes it: a rubric other than an incipit in the rubric size, plain text in
// the plain size, anything else (body text, an incipit) at size. A span of
// spaces alone does not count. 0 if p has no text.
func largestSize(p *ir.Paragraph, size bag.ScaledPoint, cfg textConfig) bag.ScaledPoint {
	var largest bag.ScaledPoint
	for _, span := range p.Content {
		if strings.TrimSpace(span.Text) == "" {
			continue
		}
		s := size
		switch {
		case span.Rubric && !span.Incipit && cfg.rubricSize > 0:
			s = cfg.rubricSize
		case span.Plain && !span.Rubric && cfg.plainFont != nil && cfg.plainSize > 0:
			s = cfg.plainSize
		}
		largest = max(largest, s)
	}
	return largest
}
