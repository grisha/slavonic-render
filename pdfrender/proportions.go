package pdfrender

import "github.com/boxesandglue/boxesandglue/backend/bag"

// The proportions of the page's type. Every size is a multiple of the body
// size and every vertical distance a fraction of the body leading
// (renderer.lead), so that the whole page follows the body size.
//
// This file is the one place a value is written. Comments and documents
// say what a proportion is for and never what it is: a number repeated
// elsewhere is wrong at its first change. The HTML style sheet
// (htmlrender/data/text.css) holds the same proportions in its own units,
// kept by hand: what is changed here is to be changed there.

// Sizes, as multiples of the body size.
const (
	titleScale       = 1.5  // the title (#)
	headingScale     = 1.2  // a heading (##)
	minorScale       = 0.85 // minor (>) paragraphs, their leading with them, and rubric (=text=) text
	furnitureScale   = 0.85 // the running header, the page number, the catchword
	plainScale       = 0.78 // plain (*text*) text
	marginNoteScale  = 0.75 // a margin note
	footnoteScale    = 0.70 // a footnote's text, and its leading as a multiple of the body's
	footnoteRefScale = 0.65 // the number of a footnote reference
	footnoteRefRise  = 0.40 // how far that number is raised
)

// leaderGap is white a leader leaves before the text after it, beyond the
// space of its pattern, as a multiple of the body size. The dots stand on a
// grid, so the last one stops one space to one space plus a copy before the
// column of page numbers (alignLeaders); one space alone reads as a dot
// jammed against the widest number. With the gap the least white is about
// that between two dots.
const leaderGap = 0.3

// Vertical distances, as fractions of the body leading.
const (
	headingSpace      = 0.6  // before a heading; a *** line
	titleSpace        = 0.5  // before the title
	rubricSpace       = 0.3  // above and below a centred line
	footnoteRuleSpace = 0.3  // around the rule over the footnotes
	ornamentSpace     = 0.15 // above an ornament
	catchwordRise     = 0.25 // the catchword, above where the next line would stand
	displayLead       = 0.2  // added to the size of a title or a heading for its leading
	lineLead          = 0.1  // the same for a one-line element: header, page number, catchword, margin note
)

// footnoteRuleDivisor divides the text's width into the length of the rule
// over the footnotes.
const footnoteRuleDivisor = 3

// leadingFor is the baseline-to-baseline distance for type of the given
// size. Looser than is customary for Latin type, because Church Slavonic
// stacks marks over its letters (an accent over a breathing, a titlo), and
// they need clearance from the line above; the marks scale with the font,
// so the clearance does too.
func leadingFor(size bag.ScaledPoint) bag.ScaledPoint {
	return size * 10 / 7
}

// dropCapMarginFor is the gap between a drop cap and its text, for type of
// the given size.
func dropCapMarginFor(size bag.ScaledPoint) bag.ScaledPoint {
	return size * 3 / 14
}
