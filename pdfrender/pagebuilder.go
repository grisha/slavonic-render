package pdfrender

import (
	"fmt"
	"os"
	"strings"

	"github.com/grisha/slavonic-render/internal/expreval"
	"github.com/grisha/slavonic-render/ir"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/document"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/boxesandglue/boxesandglue/frontend"
)

// PageNumbering is which pages are numbered; the numerals are the
// document's (PageConfig.Numeral).
type PageNumbering int

const (
	PageNumbersNone PageNumbering = iota // no page numbers
	PageNumbersAll                       // every page
	PageNumbersLeaf                      // odd pages only, by the leaf: both sides of a leaf share the number
)

// PageConfig is the page: its margin, its numbers, its catchwords.
type PageConfig struct {
	Margin        bag.ScaledPoint // on all four sides
	PageNumbering PageNumbering
	Numeral       func(int) string // writes the page numbers, the footer's and the contents' (the key Numerals)
	Catchwords    bool
}

// catchwordOnPageBreak controls whether catchwords appear after explicit === page breaks.
const catchwordOnPageBreak = false

// pendingCatchword describes the catchword to render at the next page break.
// Computed from IR by the docrenderer; rendered by the pagebuilder.
type pendingCatchword struct {
	text           string
	redFirstLetter bool   // First grapheme red, rest black (drop caps, ~ paragraphs)
	color          string // Override: entire text in this color (headings)
}

// pageBuilder manages content flow across multiple pages.
// It automatically creates new pages when content exceeds available space.
type pageBuilder struct {
	renderer *renderer
	doc      *frontend.Document

	// Page geometry (A4's width, US Letter's height)
	// This is the maximal intersection of A4 and US Letter, ensuring
	// content renders without clipping on either paper size.
	pageWidth  bag.ScaledPoint
	pageHeight bag.ScaledPoint
	margin     bag.ScaledPoint

	// Content area (accounting for margins, headers, footers)
	contentTop     bag.ScaledPoint // Y position where content starts
	contentBottom  bag.ScaledPoint // Y position where content ends
	footnoteBottom bag.ScaledPoint // Y position where footnotes can extend to (lower than contentBottom)
	headerY        bag.ScaledPoint // Y position for running header

	// State
	currentY         bag.ScaledPoint // Current Y position on page
	pageNum          int
	sectionTitle     string // Running header template (may contain ${var} refs resolved at shipout)
	isFirstPage      bool
	pageNumbering    PageNumbering    // Which pages are numbered
	numeral          func(int) string // Writes the page numbers
	catchwordEnabled bool             // Whether to show catchwords (first word of next page)

	// Catchword state
	pendingCatchword *pendingCatchword // Set by docrenderer before add* calls, consumed at page breaks

	// Per-page context: accumulated by template markers, resolved at shipout.
	// Cleared on each new page (with carry-over of last value per key).
	pageContext          map[string][]string
	currentPageHasHeader bool // set in newPage(), read in shipoutPage()

	// Vertical spacing, fractions of the body leading (renderer.lead)
	headingSpacing  bag.ScaledPoint // space before headings
	titleSpacing    bag.ScaledPoint // space after the title
	imageSpacing    bag.ScaledPoint // space before ornaments/images
	rubricSpacing   bag.ScaledPoint // space above and below centred rubric lines
	footnoteRuleGap bag.ScaledPoint // space above and below the footnote rule, together

	// Footnote area: footnotes accumulate at the bottom of the page,
	// reducing available space for body content.
	pageFootnotes      []*node.VList   // rendered footnote VLists for current page
	footnoteAreaHeight bag.ScaledPoint // total height of footnotes + rule on current page

	// Current page
	page *document.Page
}

// newPageBuilder creates a new pageBuilder for flowing content across pages.
// The PageConfig margin controls all page margins and is the single source of
// truth — text width, margin note sizing, and content placement all derive from it.
func newPageBuilder(r *renderer, doc *frontend.Document, sectionTitle string, pcfg PageConfig) *pageBuilder {
	// A4's width, US Letter's height
	pageWidth := bag.MustSP("210mm")
	pageHeight := bag.MustSP("279.4mm")

	// Set document defaults
	doc.Doc.DefaultPageWidth = pageWidth
	doc.Doc.DefaultPageHeight = pageHeight

	// Update the renderer's text width to match the margin
	textWidth := pageWidth - 2*pcfg.Margin
	r.setTextWidth(textWidth)
	r.setMargin(pcfg.Margin)

	pb := &pageBuilder{
		renderer:         r,
		doc:              doc,
		pageWidth:        pageWidth,
		pageHeight:       pageHeight,
		margin:           pcfg.Margin,
		contentBottom:    bag.MustSP("25mm"),             // Fixed bottom margin for content
		footnoteBottom:   bag.MustSP("5mm"),              // Footnotes can extend well below body content
		headerY:          pageHeight - bag.MustSP("8mm"), // Running header / page number position
		sectionTitle:     sectionTitle,
		isFirstPage:      true,
		pageNumbering:    pcfg.PageNumbering,
		numeral:          pcfg.Numeral,
		catchwordEnabled: pcfg.Catchwords,
		pageContext:      make(map[string][]string),
		headingSpacing:   r.lead(headingSpace),
		titleSpacing:     r.lead(titleSpace),
		imageSpacing:     r.lead(ornamentSpace),
		rubricSpacing:    r.lead(rubricSpace),
		footnoteRuleGap:  r.lead(footnoteRuleSpace),
	}

	return pb
}

// setRunningHeader changes the running header text displayed on subsequent pages.
func (pb *pageBuilder) setRunningHeader(text string) {
	pb.sectionTitle = text
}

// resetPageContext clears all accumulated per-page context.
// Used after section breaks (===) so the new section starts with a clean slate.
func (pb *pageBuilder) resetPageContext() {
	pb.pageContext = make(map[string][]string)
}

// pushPageContext appends value to the key's list in the per-page context.
func (pb *pageBuilder) pushPageContext(key, value string) {
	pb.pageContext[key] = append(pb.pageContext[key], value)
}

// shipoutPage completes the current page: it resolves the running header
// template against per-page context and renders the header, places the
// page's footnotes, and performs carry-over. The page itself is held, and
// shipped out by shipoutAll (see the note at the end).
func (pb *pageBuilder) shipoutPage() error {
	if pb.page == nil {
		return nil
	}

	// Render running header with per-page context resolved.
	if pb.currentPageHasHeader && pb.sectionTitle != "" {
		resolved := expreval.Expand(pb.sectionTitle, pb.pageContext)
		if strings.Contains(resolved, "${") {
			fmt.Fprintf(os.Stderr, "WARNING: unresolved expression in header `%s` on page %d, skipping\n", resolved, pb.pageNum)
		} else {
			headerVL, err := pb.renderer.header(resolved)
			if err != nil {
				return fmt.Errorf("shipoutPage: creating running header: %w", err)
			}
			pb.page.OutputAt(pb.margin, pb.headerY, headerVL)
		}
	}

	// Render accumulated footnotes at page bottom.
	if len(pb.pageFootnotes) > 0 {
		if err := pb.renderFootnoteArea(); err != nil {
			return err
		}
	}

	// Carry over: each key's last value becomes the initial value on the next page.
	nextCtx := make(map[string][]string)
	for k, v := range pb.pageContext {
		if len(v) > 0 {
			nextCtx[k] = []string{v[len(v)-1]}
		}
	}
	pb.pageContext = nextCtx

	// Note: page.Shipout() is NOT called here. All shipouts are deferred
	// to shipoutAll() so that forward-referenced content (like TOC page numbers)
	// can be filled in via OutputAt() before pages are finalized.
	return nil
}

// deferredSlot records a reserved vertical region on a held page.
// Created by reserveSpace, consumed by fillSlot.
type deferredSlot struct {
	page *document.Page
	y    bag.ScaledPoint
}

// reserveSpace advances currentY by height and returns a deferredSlot
// that records the page and Y position where content should later be placed.
// The caller should render placeholder content to measure the height, then
// call reserveSpace with that height. The actual content is placed later
// via fillSlot.
func (pb *pageBuilder) reserveSpace(height bag.ScaledPoint) deferredSlot {
	slot := deferredSlot{page: pb.page, y: pb.currentY}
	pb.currentY -= height
	return slot
}

// fillSlot places a VList on a held (not yet shipped) page at the position
// recorded in the deferredSlot. This is used after all anchors are known to
// fill in deferred content like TOC page numbers.
func (pb *pageBuilder) fillSlot(slot deferredSlot, b *node.VList) {
	slot.page.OutputAt(pb.margin, slot.y, b)
}

// addDestinationAt places a named PDF destination at position (margin, y) on
// the current page. The destination is a zero-size node that serves as the
// target for hyperlinks created with document.Hyperlink{Local: ...}.
func (pb *pageBuilder) addDestinationAt(name string, y bag.ScaledPoint) {
	if pb.page == nil {
		return
	}

	// Create start/stop pair with ActionDest
	startNode := node.NewStartStop()
	startNode.Action = node.ActionDest
	startNode.Value = name

	stopNode := node.NewStartStop()
	stopNode.StartNode = startNode

	// Chain: start → stop
	startNode.SetNext(stopNode)
	stopNode.SetPrev(startNode)

	// Wrap in HList → VList (zero-size)
	hlist := node.NewHList()
	hlist.List = startNode

	vlist := node.NewVList()
	vlist.List = hlist

	pb.page.OutputAt(pb.margin, y, vlist)
}

// shipoutAll ships out all pages in creation order. This is called at the end
// of document generation, after all deferred content has been filled in.
// Pages are iterated from the document's page list (populated by newPage in order),
// so the PDF page sequence matches document order.
func (pb *pageBuilder) shipoutAll() {
	for _, page := range pb.doc.Doc.Pages {
		if !page.Finished {
			page.Shipout()
		}
	}
}

// setPendingCatchword sets the catchword to render at the next page break.
// Called by the docrenderer before each add* call.
func (pb *pageBuilder) setPendingCatchword(cw *pendingCatchword) {
	pb.pendingCatchword = cw
}

// catchwordFromOverflow extracts the first word from rendered overflow content.
// This is the renderer-specific fallback for mid-paragraph splits where only
// the typeset output knows which word starts the next page.
func catchwordFromOverflow(overflow *node.VList) *pendingCatchword {
	text := extractFirstWordText(overflow)
	if text == "" {
		return nil
	}
	return &pendingCatchword{text: text}
}

// placeCatchword positions a rendered catchword VList at the bottom-right
// of the body content area. When footnotes are present, the catchword sits
// just above the footnote separator rule.
func (pb *pageBuilder) placeCatchword(vl *node.VList) {
	var catchwordY bag.ScaledPoint
	if pb.footnoteAreaHeight > 0 {
		// Just above the footnote area
		catchwordY = pb.footnoteBottom + pb.footnoteAreaHeight + bag.MustSP("5mm")
	} else {
		// Traditional position below content area (in the margin)
		catchwordY = pb.contentBottom - bag.MustSP("5mm") - pb.renderer.Leading + pb.renderer.lead(catchwordRise)
	}
	textWidth := pb.pageWidth - 2*pb.margin
	xPos := pb.margin + textWidth - vl.Width
	pb.page.OutputAt(xPos, catchwordY, vl)
}

// renderCatchwordFromInfo renders a pendingCatchword through the renderer and
// places it on the current page. Returns nil if cw is nil or catchwords are disabled.
func (pb *pageBuilder) renderCatchwordFromInfo(cw *pendingCatchword) error {
	if !pb.catchwordEnabled || cw == nil || cw.text == "" {
		return nil
	}

	var vl *node.VList
	var err error

	if cw.color != "" {
		vl, err = pb.renderer.catchword(cw.text, false, catchwordColor{cw.color})
	} else {
		vl, err = pb.renderer.catchword(cw.text, cw.redFirstLetter)
	}
	if err != nil {
		return fmt.Errorf("renderCatchwordFromInfo: %w", err)
	}

	pb.placeCatchword(vl)
	return nil
}

// renderPendingCatchword renders the pending catchword (if any) and clears it.
func (pb *pageBuilder) renderPendingCatchword() error {
	if pb.pendingCatchword == nil {
		return nil
	}
	cw := pb.pendingCatchword
	pb.pendingCatchword = nil
	return pb.renderCatchwordFromInfo(cw)
}

// extractFirstWordText extracts the first word as text from a VList.
// If the first word is only 1-2 grapheme clusters, it also grabs the next word
// (repeating until the result exceeds 2 grapheme clusters or no more words remain).
// It uses the Components field of glyphs to reconstruct the original text.
func extractFirstWordText(vlist *node.VList) string {
	if vlist == nil || vlist.List == nil {
		return ""
	}

	// Get the first HList (first line)
	var firstLine *node.HList
	for n := vlist.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			firstLine = hlist
			break
		}
	}
	if firstLine == nil || firstLine.List == nil {
		return ""
	}

	var text glyphText
	foundFirstGlyph := false

	cur := firstLine.List

	// Skip leading glue (indentation)
	for cur != nil {
		if _, ok := cur.(*node.Glue); ok {
			cur = cur.Next()
			continue
		}
		break
	}

	for cur != nil {
		// Collect one word
		for cur != nil {
			if _, ok := cur.(*node.Glue); ok {
				break
			}
			if glyph, ok := cur.(*node.Glyph); ok {
				foundFirstGlyph = true
				text.add(glyph)
			}
			cur = cur.Next()
		}

		if !foundFirstGlyph {
			break
		}

		// If long enough, we're done
		if countGraphemes(text.String()) > 2 {
			break
		}

		// Include space and continue to next word
		if cur == nil {
			break
		}
		if _, ok := cur.(*node.Glue); ok {
			text.addSpace()
			cur = cur.Next()
		}
	}

	return text.String()
}

// renderPageNumber adds the page number to the current page.
func (pb *pageBuilder) renderPageNumber() error {
	n := pb.pageNum
	switch pb.pageNumbering {
	case PageNumbersNone:
		return nil
	case PageNumbersLeaf:
		if pb.pageNum%2 == 0 {
			return nil // Skip even pages entirely
		}
		// Traditional Slavonic: both sides of a leaf share the same number
		// Pages 1-2 = leaf 1, pages 3-4 = leaf 2, etc.
		n = (pb.pageNum + 1) / 2
	}

	// Format the page number using the renderer
	pageNumVL, err := pb.renderer.pageNumber(pb.numeral(n))
	if err != nil {
		return fmt.Errorf("renderPageNumber: %w", err)
	}

	// Output at header Y position, in the upper right corner: out in the
	// margin, its right edge most of the way to the page's, clear of a
	// running header that fills the line
	right := pb.pageWidth - pb.margin/3
	pb.page.OutputAt(right-pageNumVL.Width, pb.headerY, pageNumVL)

	return nil
}

// newPage creates a new page and sets up content area.
// For the first page, no header is added (title serves that purpose).
// For subsequent pages, a running header is added.
func (pb *pageBuilder) newPage() error {
	pb.pageNum++
	fmt.Fprintf(os.Stderr, "  Creating page %d...\n", pb.pageNum)

	pb.page = pb.doc.Doc.NewPage()

	if pb.isFirstPage {
		// First page: content starts below title area (fixed vertical offset)
		pb.contentTop = pb.pageHeight - bag.MustSP("22mm")
		pb.isFirstPage = false
		pb.currentPageHasHeader = false
	} else {
		// Subsequent pages: header is rendered at shipout time (deferred so
		// that per-page context ${var} references can be resolved).
		// Reserve space for the header now: the text starts two leadings
		// below the header's top, a type distance, so that it follows the
		// body size (the header's own place, headerY, is the page's).
		pb.contentTop = pb.headerY - pb.renderer.lead(2)
		pb.currentPageHasHeader = true
	}

	// Add page number (if configured)
	if err := pb.renderPageNumber(); err != nil {
		return err
	}

	pb.currentY = pb.contentTop
	pb.pageFootnotes = nil
	pb.footnoteAreaHeight = 0
	return nil
}

// footnoteBodyReduction returns how much footnotes reduce the body content area.
// Footnotes first fill the extra margin space (contentBottom - footnoteBottom)
// before eating into body content.
func (pb *pageBuilder) footnoteBodyReduction() bag.ScaledPoint {
	extraMargin := pb.contentBottom - pb.footnoteBottom
	if pb.footnoteAreaHeight <= extraMargin {
		return 0
	}
	return pb.footnoteAreaHeight - extraMargin
}

// availableHeight returns the remaining vertical space on the current page.
// This accounts for space reserved by footnotes at the page bottom.
func (pb *pageBuilder) availableHeight() bag.ScaledPoint {
	return pb.currentY - pb.contentBottom - pb.footnoteBodyReduction()
}

// footnoteSpaceNeeded calculates how much vertical space the given footnotes
// would consume, including the separator rule if this would be the first
// footnote on the page.
func (pb *pageBuilder) footnoteSpaceNeeded(footnoteVLists []*node.VList) bag.ScaledPoint {
	var total bag.ScaledPoint
	for _, vl := range footnoteVLists {
		total += vl.Height + vl.Depth
	}
	if len(pb.pageFootnotes) == 0 {
		rule, err := pb.renderer.footnoteRule()
		if err == nil {
			total += rule.Height + rule.Depth + pb.footnoteRuleGap
		}
	}
	return total
}

// registerFootnotes reserves space at the bottom of the current page for the
// given footnote VLists. Returns true if they fit; false if a page break is needed.
// The caller should handle the page break and retry.
func (pb *pageBuilder) registerFootnotes(footnoteVLists []*node.VList) bool {
	needed := pb.footnoteSpaceNeeded(footnoteVLists)

	if needed > pb.availableHeight() {
		return false
	}

	pb.pageFootnotes = append(pb.pageFootnotes, footnoteVLists...)
	pb.footnoteAreaHeight += needed
	return true
}

// renderFootnoteArea places the footnote separator rule and all accumulated
// footnote VLists at the bottom of the current page.
func (pb *pageBuilder) renderFootnoteArea() error {
	// Starting Y: footnotes extend down to footnoteBottom, so the top of the
	// footnote area is footnoteBottom + footnoteAreaHeight.
	y := pb.footnoteBottom + pb.footnoteAreaHeight

	// Render the separator rule.
	rule, err := pb.renderer.footnoteRule()
	if err != nil {
		return fmt.Errorf("renderFootnoteArea: %w", err)
	}
	y -= pb.footnoteRuleGap / 2
	pb.page.OutputAt(pb.margin, y, rule)
	y -= rule.Height + rule.Depth + pb.footnoteRuleGap/2

	// Render each footnote below the rule.
	for _, vl := range pb.pageFootnotes {
		pb.page.OutputAt(pb.margin, y, vl)
		y -= vl.Height + vl.Depth
	}

	return nil
}

// addTitle adds a title to the first page.
// This should be called before any body content.
func (pb *pageBuilder) addTitle(title string) error {
	if pb.page == nil {
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	titleVL, err := pb.renderer.title(title)
	if err != nil {
		return fmt.Errorf("AddTitle: %w", err)
	}

	pb.page.OutputAt(pb.margin, pb.currentY, titleVL)
	pb.currentY -= titleVL.Height + titleVL.Depth + pb.titleSpacing

	return nil
}

// dropCapFit describes how a drop cap paragraph will be placed.
type dropCapFit int

const (
	dropCapFitWhole    dropCapFit = iota // Entire paragraph fits on current page
	dropCapFitPushNext                   // Entire paragraph pushed to next page
	dropCapFitSplit                      // Paragraph split across pages
)

// predictDropCapFit determines how addDropCap will place the given result
// without actually doing it. This lets callers (e.g., margin note placement)
// act before pages are shipped out.
func (pb *pageBuilder) predictDropCapFit(result *dropCapResult) dropCapFit {
	bodyHeight := result.body.Height + result.body.Depth
	dropCapHeight := result.dropCap.Height + result.dropCap.Depth
	dropCapBottom := dropCapHeight - result.dropCapOffset
	totalHeight := bodyHeight
	if dropCapBottom > totalHeight {
		totalHeight = dropCapBottom
	}

	available := pb.availableHeight()

	if totalHeight <= available {
		return dropCapFitWhole
	}

	dropCapLines := pb.renderer.DropCap.Lines
	minKeepHeight := indentedLinesHeight(result.body, dropCapLines)
	if dropCapBottom > minKeepHeight {
		minKeepHeight = dropCapBottom
	}

	if available < minKeepHeight {
		return dropCapFitPushNext
	}

	return dropCapFitSplit
}

// addDropCap adds a drop cap paragraph to the page.
// This handles the special positioning required for drop caps.
func (pb *pageBuilder) addDropCap(result *dropCapResult) error {
	return pb.addDropCapWithOverflowHandler(result, nil)
}

// addDropCapWithOverflowHandler is like addDropCap but calls handler for each
// overflow page. The handler receives cumulative heights measured from the top
// of the original body VList (not just the overflow portion), so callers can
// use the same YOffset values from resolveMarginNotes directly.
func (pb *pageBuilder) addDropCapWithOverflowHandler(result *dropCapResult, handler overflowPageFunc) error {
	if pb.page == nil {
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	bodyVL := result.body
	dropCapVL := result.dropCap

	bodyHeight := bodyVL.Height + bodyVL.Depth

	// Compute how far the drop cap actually extends below currentY.
	// The drop cap is placed at currentY + DropCapOffset (offset moves it up),
	// so its bottom is at: currentY + DropCapOffset - (DropCap.Height + DropCap.Depth).
	// The distance below currentY is: -(DropCapOffset) + DropCap.Height + DropCap.Depth,
	// i.e. dropCapHeight - DropCapOffset.
	dropCapHeight := dropCapVL.Height + dropCapVL.Depth
	dropCapBottom := dropCapHeight - result.dropCapOffset
	totalHeight := bodyHeight
	if dropCapBottom > totalHeight {
		totalHeight = dropCapBottom
	}

	// Check if the entire drop cap paragraph fits
	if totalHeight <= pb.availableHeight() {
		// It fits - output the body and drop cap
		pb.page.OutputAt(pb.margin, pb.currentY, bodyVL)
		pb.page.OutputAt(pb.margin, pb.currentY+result.dropCapOffset, dropCapVL)
		pb.currentY -= totalHeight
		return nil
	}

	// Figure out how many lines the drop cap spans (indented lines).
	// We must not split within these lines — the indented lines only make
	// sense visually when the drop cap letter is next to them.
	dropCapLines := pb.renderer.DropCap.Lines

	// Compute the minimum height needed to keep the drop cap and its
	// indented lines together.
	minKeepHeight := indentedLinesHeight(bodyVL, dropCapLines)
	if dropCapBottom > minKeepHeight {
		minKeepHeight = dropCapBottom
	}

	available := pb.availableHeight()

	if available < minKeepHeight {
		// Not enough room for the drop cap + its indented lines.
		// Push the entire drop cap paragraph to the next page.
		if err := pb.renderPendingCatchword(); err != nil {
			return err
		}
		if err := pb.shipoutPage(); err != nil {
			return err
		}
		if err := pb.newPage(); err != nil {
			return err
		}
		return pb.addDropCapWithOverflowHandler(result, handler)
	}

	// We can fit at least the indented lines. Split the rest normally.
	fits, overflow := splitVList(bodyVL, available)

	if fits != nil {
		pb.page.OutputAt(pb.margin, pb.currentY, fits)
		pb.page.OutputAt(pb.margin, pb.currentY+result.dropCapOffset, dropCapVL)

		// Add catchword before shipping out (must precede patchOverflowColor
		// which modifies the overflow's first HList).
		cw := catchwordFromOverflow(overflow)
		patchOverflowColor(fits, overflow)
		if err := pb.renderCatchwordFromInfo(cw); err != nil {
			return err
		}

		// Ship out current page and start new one
		if err := pb.shipoutPage(); err != nil {
			return err
		}
		if err := pb.newPage(); err != nil {
			return err
		}

		// Continue adding the overflow (these are full-width lines, no indent).
		// Wrap the handler to adjust cumulative heights: the overflow handler
		// measures from the start of the overflow, but the caller's notes have
		// YOffsets from the start of the full body VList.
		if overflow != nil {
			var adjustedHandler overflowPageFunc
			if handler != nil {
				fitsH := fits.Height // internal height only, matches computeLineYOffsets
				adjustedHandler = func(cumHeight, pageHeight bag.ScaledPoint) error {
					return handler(fitsH+cumHeight, pageHeight)
				}
			}
			return pb.addVList(overflow, adjustedHandler)
		}
	} else {
		// Nothing fits — start a new page
		if err := pb.shipoutPage(); err != nil {
			return err
		}
		if err := pb.newPage(); err != nil {
			return err
		}
		return pb.addDropCapWithOverflowHandler(result, handler)
	}

	return nil
}

// addDropCapParagraph formats and adds a drop cap paragraph, re-formatting without
// hyphenation if the paragraph has hyphenated lines and will span pages.
// This prevents cross-page hyphenation.
func (pb *pageBuilder) addDropCapParagraph(spans []ir.Span, overrides ...any) error {
	if pb.page == nil {
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	// Format drop cap paragraph normally (with hyphenation enabled)
	result, err := pb.renderer.dropCapParagraph(spans, overrides...)
	if err != nil {
		return fmt.Errorf("AddDropCapParagraph: %w", err)
	}

	height := result.body.Height + result.body.Depth
	hasHyphens := countHyphenatedLines(result.body) > 0
	willSpanPages := height > pb.availableHeight()

	// If has hyphens and will span pages, re-format without hyphenation
	if hasHyphens && willSpanPages {
		fmt.Fprintf(os.Stderr, "  Re-formatting drop cap paragraph without hyphenation (has %d hyphenated lines, height=%v > available=%v)\n",
			countHyphenatedLines(result.body), height, pb.availableHeight())

		// Create new overrides with HyphenPenalty=10000 to disable hyphenation
		newOverrides := append([]any{renderConfig{hyphenPenalty: 10000}}, overrides...)
		result, err = pb.renderer.dropCapParagraph(spans, newOverrides...)
		if err != nil {
			return fmt.Errorf("AddDropCapParagraph: re-formatting without hyphenation: %w", err)
		}
	}

	return pb.addDropCap(result)
}

// overflowPageFunc is called for each page during addWithOverflowHandler.
// cumulativeHeight is the total content height placed on all previous pages.
// pageHeight is the content height that will be placed on this page.
// The function is called before content is output on the page, allowing the
// caller to place additional content (like margin notes) on each page.
type overflowPageFunc func(cumulativeHeight, pageHeight bag.ScaledPoint) error

// add adds a VList (paragraph or other vertical content) to the page.
// If the content doesn't fit, it's split across pages.
func (pb *pageBuilder) add(b *node.VList) error {
	return pb.addVList(b, nil)
}

// addWithOverflowHandler is like add but calls handler for each page during
// a multi-page split. The handler is called for every page (including the
// first), after the split determines how much fits, and before the content is
// output. This allows callers to place content (e.g., margin notes) on each
// page at the correct positions.
func (pb *pageBuilder) addWithOverflowHandler(b *node.VList, handler overflowPageFunc) error {
	return pb.addVList(b, handler)
}

// addVList is the internal implementation for add/addWithOverflowHandler.
func (pb *pageBuilder) addVList(vlist *node.VList, handler overflowPageFunc) error {
	if pb.page == nil {
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	height := vlist.Height + vlist.Depth

	// Does it fit on the current page?
	if height <= pb.availableHeight() {
		if handler != nil {
			if err := handler(0, height); err != nil {
				return err
			}
		}
		pb.page.OutputAt(pb.margin, pb.currentY, vlist)
		pb.currentY -= height
		return nil
	}

	// Need to split across pages
	var cumulativeHeight bag.ScaledPoint

	for vlist != nil {
		available := pb.availableHeight()

		// If less than one line of space remains, treat as full page.
		// This avoids orphan lines at the page bottom and ensures the
		// pending catchword (set from IR) fires instead of the
		// overflow-based fallback.
		if available < pb.renderer.Leading {
			if err := pb.renderPendingCatchword(); err != nil {
				return err
			}
			if err := pb.shipoutPage(); err != nil {
				return err
			}
			if err := pb.newPage(); err != nil {
				return err
			}
			continue
		}

		fits, overflow := splitVList(vlist, available)

		if fits != nil {
			// fits.Height from splitVList = sum of internal node heights
			// (hlist.H+D, glue, kern) — matches computeLineYOffsets.
			// fits.Depth = max HList depth — additional layout spacing.
			// Use fits.Height for handler/cumulative tracking (must match
			// YOffset values from resolveMarginNotes), and the full
			// Height+Depth for page layout positioning.
			contentHeight := fits.Height
			layoutHeight := fits.Height + fits.Depth

			// Call handler before outputting content on this page
			if handler != nil {
				if err := handler(cumulativeHeight, contentHeight); err != nil {
					return err
				}
			}

			pb.page.OutputAt(pb.margin, pb.currentY, fits)
			pb.currentY -= layoutHeight
			cumulativeHeight += contentHeight
		}

		if overflow != nil && vlistHasLines(overflow) {
			// Add catchword before shipping out (must precede patchOverflowColor
			// which modifies the overflow's first HList).
			cw := catchwordFromOverflow(overflow)
			patchOverflowColor(fits, overflow)
			if err := pb.renderCatchwordFromInfo(cw); err != nil {
				return err
			}
			// Ship out current page and continue
			if err := pb.shipoutPage(); err != nil {
				return err
			}
			if err := pb.newPage(); err != nil {
				return err
			}
			vlist = overflow
		} else {
			// All content placed (overflow is nil or just trailing glue)
			break
		}
	}

	return nil
}

// placeInLeftMargin places a VList in the left margin at the given Y coordinate.
// The VList should be right-aligned internally (e.g., formatted with HAlignRight).
// It is positioned so its right edge sits at (margin - gap).
func (pb *pageBuilder) placeInLeftMargin(b *node.VList, y bag.ScaledPoint) {
	if pb.page == nil || b == nil {
		return
	}
	xPos := pb.margin - marginNoteGap - b.Width
	if xPos < 0 {
		xPos = 0
	}
	pb.page.OutputAt(xPos, y, b)
}

// addHeadingSpace adds standard pre-heading spacing.
func (pb *pageBuilder) addHeadingSpace() {
	pb.currentY -= pb.headingSpacing
}

// addImageSpace adds standard pre-image/ornament spacing.
func (pb *pageBuilder) addImageSpace() {
	pb.currentY -= pb.imageSpacing
}

// addRubricSpace adds the spacing placed above and below a centred rubric line.
func (pb *pageBuilder) addRubricSpace() {
	pb.currentY -= pb.rubricSpacing
}

// breakPage forces a page break and starts a new page.
// If a pending catchword has been set, it is rendered before shipping out.
func (pb *pageBuilder) breakPage() error {
	if pb.page != nil {
		if err := pb.renderPendingCatchword(); err != nil {
			return err
		}
		if err := pb.shipoutPage(); err != nil {
			return err
		}
	}
	return pb.newPage()
}

// headingFitsOnPage reports whether a heading VList would be placed on the
// current page by addHeading (i.e., there is enough room for the heading
// plus at least two lines of body text). Returns false when no page exists.
func (pb *pageBuilder) headingFitsOnPage(b *node.VList) bool {
	if pb.page == nil || b == nil {
		return false
	}
	height := b.Height + b.Depth
	return height+2*pb.renderer.Leading <= pb.availableHeight()
}

// addHeading adds a heading VList to the page without splitting.
// If the heading doesn't fit on the current page, it's pushed to the next page.
func (pb *pageBuilder) addHeading(vlist *node.VList) error {
	if pb.page == nil {
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	height := vlist.Height + vlist.Depth

	// If the heading plus at least two lines of body text doesn't fit,
	// start a new page (avoids a stranded heading at the bottom).
	if height+2*pb.renderer.Leading > pb.availableHeight() {
		if err := pb.renderPendingCatchword(); err != nil {
			return err
		}
		if err := pb.shipoutPage(); err != nil {
			return err
		}
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	pb.page.OutputAt(pb.margin, pb.currentY, vlist)
	pb.currentY -= height
	return nil
}

// addOrnament adds an ornament to the page, by its name, centered
// horizontally. Optional ornamentConfig can override width and centering.
func (pb *pageBuilder) addOrnament(name string, overrides ...any) error {
	if pb.page == nil {
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	ornamentVL, err := pb.renderer.ornament(name, overrides...)
	if err != nil {
		return fmt.Errorf("AddOrnament: %w", err)
	}

	// Check if ornament fits on current page
	if ornamentVL.Height > pb.availableHeight() {
		// No text catchword for ornaments — clear pending
		pb.pendingCatchword = nil
		if err := pb.shipoutPage(); err != nil {
			return err
		}
		if err := pb.newPage(); err != nil {
			return err
		}
	}

	// Center the ornament within text width
	textWidth := pb.pageWidth - 2*pb.margin
	ornamentX := pb.margin + (textWidth-ornamentVL.Width)/2

	pb.page.OutputAt(ornamentX, pb.currentY, ornamentVL)
	pb.currentY -= ornamentVL.Height + ornamentVL.Depth

	return nil
}

// finish renders the running header on the final page, ships out all pages
// in creation order, and completes the document.
func (pb *pageBuilder) finish() error {
	// Render the running header on the last page (shipoutPage does headers
	// and context carry-over; shipoutAll then ships the pages out).
	if err := pb.shipoutPage(); err != nil {
		return err
	}
	pb.shipoutAll()
	return pb.doc.Doc.Finish()
}

// splitVList splits a VList at a height boundary.
// Returns (fits, overflow) where fits contains content up to maxHeight,
// and overflow contains the remaining content.
// Split points are chosen between HLists (lines) for clean breaks.
func splitVList(vlist *node.VList, maxHeight bag.ScaledPoint) (fits, overflow *node.VList) {
	if vlist == nil {
		return nil, nil
	}

	// If the entire VList fits, return it
	totalHeight := vlist.Height + vlist.Depth
	if totalHeight <= maxHeight {
		return vlist, nil
	}

	// Walk through the nodes to find a split point
	var accumulatedHeight bag.ScaledPoint
	var lastValidSplit node.Node
	var nodeCount int

	for n := vlist.List; n != nil; n = n.Next() {
		nodeCount++
		var nodeHeight bag.ScaledPoint

		if hlist, ok := n.(*node.HList); ok {
			nodeHeight = hlist.Height + hlist.Depth
		} else if vl, ok := n.(*node.VList); ok {
			nodeHeight = vl.Height + vl.Depth
		} else if glue, ok := n.(*node.Glue); ok {
			nodeHeight = glue.Width // Glue width is the stretch in vertical mode
		} else if kern, ok := n.(*node.Kern); ok {
			nodeHeight = kern.Kern
		}
		// Skip other node types (they have negligible height)

		if accumulatedHeight+nodeHeight > maxHeight {
			// This node doesn't fit
			break
		}

		accumulatedHeight += nodeHeight
		lastValidSplit = n
	}

	// If we can't fit even one line, force at least one
	if lastValidSplit == nil {
		// Take the first node and put everything else in overflow
		if vlist.List != nil {
			lastValidSplit = vlist.List
		} else {
			return nil, vlist
		}
	}

	// Back up past any hyphenated lines, so that a page does not end in a
	// hyphen. A drop cap paragraph that spans pages was set again without
	// hyphenation (addDropCapParagraph); every other paragraph comes here as
	// it was set, and this is what keeps its hyphen off the foot of a page.
	for i := 0; i < 3; i++ {
		if hlist, ok := lastValidSplit.(*node.HList); ok {
			if lineEndsWithHyphen(hlist) && lastValidSplit.Prev() != nil {
				// Back up to previous HList
				for prev := lastValidSplit.Prev(); prev != nil; prev = prev.Prev() {
					if _, ok := prev.(*node.HList); ok {
						lastValidSplit = prev
						break
					}
				}
			} else {
				break // No hyphen, we're good
			}
		} else {
			break
		}
	}

	// Create the "fits" VList with nodes up to the split point
	fits = &node.VList{}
	var fitsHeight, fitsDepth bag.ScaledPoint

	for n := vlist.List; n != nil; {
		next := n.Next()

		// Measure this node
		if hlist, ok := n.(*node.HList); ok {
			fitsHeight += hlist.Height + hlist.Depth
			if hlist.Depth > fitsDepth {
				fitsDepth = hlist.Depth
			}
		} else if vl, ok := n.(*node.VList); ok {
			fitsHeight += vl.Height + vl.Depth
		} else if glue, ok := n.(*node.Glue); ok {
			fitsHeight += glue.Width
		} else if kern, ok := n.(*node.Kern); ok {
			fitsHeight += kern.Kern
		}

		// Detach from original list and append to fits
		n.SetNext(nil)
		n.SetPrev(nil)
		appendNode(fits, n)

		if n == lastValidSplit {
			// Create overflow with remaining nodes
			if next != nil {
				overflow = &node.VList{List: next}
				next.SetPrev(nil)

				// Calculate overflow dimensions
				var overflowHeight bag.ScaledPoint
				for on := next; on != nil; on = on.Next() {
					if hlist, ok := on.(*node.HList); ok {
						overflowHeight += hlist.Height + hlist.Depth
					} else if vl, ok := on.(*node.VList); ok {
						overflowHeight += vl.Height + vl.Depth
					} else if glue, ok := on.(*node.Glue); ok {
						overflowHeight += glue.Width
					} else if kern, ok := on.(*node.Kern); ok {
						overflowHeight += kern.Kern
					}
				}
				overflow.Height = overflowHeight
			}
			break
		}

		n = next
	}

	fits.Height = fitsHeight
	fits.Depth = fitsDepth

	return fits, overflow
}

// colorResetPDF is the PDF command that resets both stroking and non-stroking
// color to black. boxesandglue emits this from color-stop StartStop callbacks.
const colorResetPDF = "0 0 0 RG 0 0 0 rg "

// patchOverflowColor ensures color continuity across a splitVList boundary.
// When a paragraph is split mid-color-span, the overflow HLists lack the
// color-start StartStop node (it lives in an earlier HList that stayed in fits).
// This walks fits to find the last active color callback. If still active
// (not reset to black), it injects a matching color-start StartStop at the
// beginning of the overflow's first HList.
func patchOverflowColor(fits, overflow *node.VList) {
	if fits == nil || overflow == nil {
		return
	}

	// Walk all HLists in fits, tracking the current color state.
	currentColor := colorResetPDF
	for n := fits.List; n != nil; n = n.Next() {
		hlist, ok := n.(*node.HList)
		if !ok {
			continue
		}
		for hn := hlist.List; hn != nil; hn = hn.Next() {
			if ss, ok := hn.(*node.StartStop); ok && ss.ShipoutCallback != nil {
				currentColor = ss.ShipoutCallback(ss)
			}
		}
	}

	if currentColor == colorResetPDF {
		return // Color was reset to black — nothing to patch.
	}

	// Find the first HList in overflow.
	var firstLine *node.HList
	for n := overflow.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			firstLine = hlist
			break
		}
	}
	if firstLine == nil {
		return
	}

	// Inject a color-start StartStop at the head of the first overflow line.
	colorStr := currentColor // capture for closure
	colorStart := node.NewStartStop()
	colorStart.Position = node.PDFOutputPage
	colorStart.ShipoutCallback = func(n node.Node) string {
		return colorStr
	}

	oldHead := firstLine.List
	firstLine.List = colorStart
	colorStart.SetNext(oldHead)
	if oldHead != nil {
		oldHead.SetPrev(colorStart)
	}
}

// appendNode appends a node to a VList.
func appendNode(vlist *node.VList, n node.Node) {
	if vlist.List == nil {
		vlist.List = n
		return
	}

	// Find the last node
	last := vlist.List
	for last.Next() != nil {
		last = last.Next()
	}
	last.SetNext(n)
	n.SetPrev(last)
}

// indentedLinesHeight computes the total height of the first n HList lines
// (plus any interline glue/kern between them) in a VList. This is used to
// determine the minimum space needed to keep a drop cap and its indented
// lines on the same page.
func indentedLinesHeight(vlist *node.VList, n int) bag.ScaledPoint {
	var height bag.ScaledPoint
	linesSeen := 0
	for nd := vlist.List; nd != nil; nd = nd.Next() {
		if hlist, ok := nd.(*node.HList); ok {
			linesSeen++
			height += hlist.Height + hlist.Depth
			if linesSeen >= n {
				break
			}
		} else if glue, ok := nd.(*node.Glue); ok {
			height += glue.Width
		} else if kern, ok := nd.(*node.Kern); ok {
			height += kern.Kern
		}
	}
	return height
}

// vlistHasLines reports whether the VList contains at least one HList (text line).
// Used to distinguish real content overflow from trailing glue/kern only.
func vlistHasLines(vlist *node.VList) bool {
	if vlist == nil {
		return false
	}
	for n := vlist.List; n != nil; n = n.Next() {
		if _, ok := n.(*node.HList); ok {
			return true
		}
	}
	return false
}

// countHyphenatedLines returns the number of lines ending with a hyphen in a VList.
func countHyphenatedLines(vlist *node.VList) int {
	count := 0
	for n := vlist.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			if lineEndsWithHyphen(hlist) {
				count++
			}
		}
	}
	return count
}

// lineEndsWithHyphen checks if an HList (line) ends with a hyphen.
// This is used to prevent cross-page hyphenation.
func lineEndsWithHyphen(hlist *node.HList) bool {
	if hlist == nil || hlist.List == nil {
		return false
	}

	// Find the last glyph in the line (skip trailing glue/kern)
	var lastGlyph *node.Glyph
	for n := hlist.List; n != nil; n = n.Next() {
		if glyph, ok := n.(*node.Glyph); ok {
			lastGlyph = glyph
		}
	}

	if lastGlyph == nil {
		return false
	}

	// Check if it's a hyphen
	// The hyphen-minus character is U+002D (codepoint 45), but could also be U+2010 (hyphen)
	if lastGlyph.Components == "-" || lastGlyph.Components == "‐" {
		return true
	}
	// Check codepoint directly - hyphen-minus is 45 in most fonts
	if lastGlyph.Codepoint == 45 {
		return true
	}

	return false
}
