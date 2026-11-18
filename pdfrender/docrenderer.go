package pdfrender

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/node"

	"github.com/grisha/slavonic-render/cu"
	"github.com/grisha/slavonic-render/internal/expreval"
	"github.com/grisha/slavonic-render/ir"
)

// DocumentRenderer sets an ir.Document on the pages of a PDF. Made by
// NewDocumentFromConfig, it is used in this order: Render; RenderDeferred
// if HasDeferred; Finish, which writes the file.
type DocumentRenderer struct {
	renderer    *renderer
	pageBuilder *pageBuilder

	anchorPages   map[string]int    // anchor name → page number
	pendingAnchor string            // anchor waiting to attach to next heading
	deferred      []deferredIR      // blocks with unresolved expressions
	exprVars      map[string]string // front matter variables for expression evaluation
	exprFuncs     map[string]any    // custom functions for expression evaluation
	afterRubric   bool              // last visible block was a centred rubric (suppresses doubled rubric space)

	// Footnotes: keyed by display number for quick lookup.
	footnoteByNum map[int]ir.FootnoteDef
}

// deferredIR records an IR block that could not be rendered because it contains
// unresolved ${...} expressions.
type deferredIR struct {
	block ir.Block
	slot  deferredSlot
}

// newDocumentRenderer creates a new DocumentRenderer.
// fmVars are front matter variables made available in ${...} expressions,
// each the text of its key.
func newDocumentRenderer(r *renderer, pb *pageBuilder, fmVars map[string]string) *DocumentRenderer {
	dr := &DocumentRenderer{
		renderer:    r,
		pageBuilder: pb,
		anchorPages: make(map[string]int),
		exprVars:    fmVars,
	}
	dr.exprFuncs = dr.buildExprFuncs()
	return dr
}

// buildExprFuncs constructs the expression function map.
func (dr *DocumentRenderer) buildExprFuncs() map[string]any {
	return map[string]any{
		"page": dr.exprPage,
	}
}

// exprPage resolves an anchor name to its page number, in the document's
// numerals.
func (dr *DocumentRenderer) exprPage(name string) (string, error) {
	pageNum, ok := dr.anchorPages[name]
	if !ok {
		return "", fmt.Errorf("exprPage: anchor %q not yet placed: %w", name, expreval.ErrNeedsDefer)
	}
	return dr.pageBuilder.numeral(pageNum), nil
}

// Render sets the blocks of doc on pages, in order. A block with an
// expression that names an anchor not yet placed (a page number in the
// contents) waits for RenderDeferred.
func (dr *DocumentRenderer) Render(doc *ir.Document) error {
	// Build footnote lookup by display number.
	if len(doc.Footnotes) > 0 {
		dr.footnoteByNum = make(map[int]ir.FootnoteDef, len(doc.Footnotes))
		for _, fn := range doc.Footnotes {
			dr.footnoteByNum[fn.Num] = fn
		}
	}

	blocks := doc.Blocks
	for i := 0; i < len(blocks); i++ {
		if err := dr.renderBlock(blocks[i], blocks[i+1:]); err != nil {
			return err
		}
	}
	return nil
}

// RenderDeferred resolves all blocks that were deferred because an
// expression of theirs named an anchor not yet placed. Must be called after
// Render(). Every anchor of the document is placed by now, so one still
// missing is nowhere in it: an error naming the expression.
func (dr *DocumentRenderer) RenderDeferred() error {
	if len(dr.deferred) == 0 {
		return nil
	}
	for _, d := range dr.deferred {
		b, err := dr.resolveAndRenderBlock(d.block)
		if errors.Is(err, expreval.ErrNeedsDefer) {
			return fmt.Errorf("RenderDeferred: an anchor that is not in the document: %w", err)
		}
		if err != nil {
			return fmt.Errorf("RenderDeferred: %w", err)
		}
		if b == nil {
			continue
		}
		dr.pageBuilder.fillSlot(d.slot, b)
	}
	dr.deferred = nil
	return nil
}

// HasDeferred returns true if there are blocks waiting for expression resolution.
func (dr *DocumentRenderer) HasDeferred() bool {
	return len(dr.deferred) > 0
}

// Finish finalises the PDF document (ships out the last page, writes output).
func (dr *DocumentRenderer) Finish() error {
	return dr.pageBuilder.finish()
}

// renderBlock dispatches a single IR block to the appropriate renderer.
// remaining is the slice of blocks after the current one (for look-ahead).
func (dr *DocumentRenderer) renderBlock(block ir.Block, remaining []ir.Block) error {
	afterRubric := dr.afterRubric
	switch block.Kind {
	case ir.BlockPageContextOp, ir.BlockAnchor, ir.BlockRunningHeader:
		// Invisible blocks do not break a run of rubrics.
	default:
		dr.afterRubric = block.Kind == ir.BlockCentered
	}
	switch block.Kind {
	case ir.BlockParagraph:
		return dr.renderParagraph(block)
	case ir.BlockCentered:
		return dr.renderCenteredParagraph(block, afterRubric)
	case ir.BlockDropCap:
		return dr.renderDropCapParagraph(block)
	case ir.BlockHeading1:
		return dr.renderTitle(block)
	case ir.BlockHeading2:
		return dr.renderHeading(block)
	case ir.BlockImage:
		return dr.renderImage(block)
	case ir.BlockRunningHeader:
		dr.pageBuilder.setRunningHeader(block.Text)
		return nil
	case ir.BlockPageBreak:
		if catchwordOnPageBreak {
			if next := findNextVisibleBlock(remaining); next != nil {
				dr.setCatchwordForBlock(next)
			}
		} else {
			// Clear any stale pending catchword from a prior block
			// that fit on the page without triggering a break.
			dr.pageBuilder.setPendingCatchword(nil)
		}
		if err := dr.pageBuilder.breakPage(); err != nil {
			return err
		}
		dr.pageBuilder.resetPageContext()
		return nil
	case ir.BlockThematicBreak:
		dr.pageBuilder.addHeadingSpace()
		return nil
	case ir.BlockPageContextOp:
		dr.pageBuilder.pushPageContext(block.Key, block.Value)
		return nil
	case ir.BlockAnchor:
		dr.pendingAnchor = block.Text
		return nil
	}
	return nil
}

// renderParagraph renders a regular body paragraph.
func (dr *DocumentRenderer) renderParagraph(block ir.Block) error {
	spans := block.Content
	if len(spans) == 0 {
		return nil
	}

	// Check if any spans have unresolved expressions.
	if hasExprSpans(spans) {
		resolved, err := dr.resolveSpans(spans)
		if errors.Is(err, expreval.ErrNeedsDefer) {
			return dr.deferBlock(block, err)
		}
		if err != nil {
			return err
		}
		spans = resolved
	}

	footnotes, err := dr.preRenderFootnotes(spans)
	if err != nil {
		return err
	}

	dr.setCatchwordFromSpans(spans)

	para := ir.Paragraph{Content: spans, Indented: block.Indented, Minor: block.Minor}
	b, err := dr.renderer.renderParagraph(para)
	if err != nil {
		return fmt.Errorf("renderParagraph: %w", err)
	}

	return dr.addBlockWithFootnotes(b, footnotes, block.Notes)
}

// renderCenteredParagraph renders a centered paragraph (a rubric line). Rubrics
// get a small space above and below so they stand off from body text and so a
// descender (Ѱ) clears a following drop cap. Consecutive rubrics share one gap:
// the space above is skipped when the previous visible block was also a rubric.
func (dr *DocumentRenderer) renderCenteredParagraph(block ir.Block, afterRubric bool) error {
	spans := block.Content
	if len(spans) == 0 {
		return nil
	}

	if hasExprSpans(spans) {
		resolved, err := dr.resolveSpans(spans)
		if errors.Is(err, expreval.ErrNeedsDefer) {
			// A line that waits takes the room of one that does not: the
			// rubric's space above and below, as further down.
			if !afterRubric {
				dr.pageBuilder.addRubricSpace()
			}
			if err := dr.deferBlock(block, err); err != nil {
				return err
			}
			dr.pageBuilder.addRubricSpace()
			return nil
		}
		if err != nil {
			return err
		}
		spans = resolved
	}

	footnotes, err := dr.preRenderFootnotes(spans)
	if err != nil {
		return err
	}

	dr.setCatchwordFromSpans(spans)

	para := ir.Paragraph{Content: spans, Centered: true, Minor: block.Minor}
	b, err := dr.renderer.renderParagraph(para)
	if err != nil {
		return fmt.Errorf("renderCenteredParagraph: %w", err)
	}

	if !afterRubric {
		dr.pageBuilder.addRubricSpace()
	}
	if len(footnotes) > 0 {
		err = dr.addBlockWithFootnotes(b, footnotes, block.Notes)
	} else {
		err = dr.pageBuilder.addHeadingWithNotes(b, block.Notes)
	}
	if err != nil {
		return err
	}
	dr.pageBuilder.addRubricSpace()
	return nil
}

// renderDropCapParagraph renders a drop cap paragraph.
func (dr *DocumentRenderer) renderDropCapParagraph(block ir.Block) error {
	spans := block.Content
	if len(spans) == 0 {
		return nil
	}

	if hasExprSpans(spans) {
		resolved, err := dr.resolveSpans(spans)
		if errors.Is(err, expreval.ErrNeedsDefer) {
			// A deferred block is set as a paragraph in room reserved for
			// it, which a drop cap and its text are not.
			return fmt.Errorf("renderDropCapParagraph: a drop cap paragraph cannot wait for a page not yet made: %w", err)
		}
		if err != nil {
			return err
		}
		spans = resolved
	}

	footnotes, err := dr.preRenderFootnotes(spans)
	if err != nil {
		return err
	}

	dr.setCatchwordFromDropCap(spans)

	var overrides []any
	if block.Minor {
		overrides = append(overrides, renderConfig{
			bodySize: bag.ScaledPoint(float64(dr.renderer.BodySize) * minorScale),
			leading:  bag.ScaledPoint(float64(dr.renderer.Leading) * minorScale),
		})
	}

	if len(footnotes) > 0 {
		// For drop caps with footnotes, render the result and use addBlockWithFootnotes.
		result, err := dr.renderer.dropCapParagraph(spans, overrides...)
		if err != nil {
			return fmt.Errorf("renderDropCapParagraph: %w", err)
		}
		// Register footnotes with combined fit check, then add drop cap.
		if err := dr.reserveFootnotes(result.body.Height+result.body.Depth, footnotes); err != nil {
			return err
		}
		if len(block.Notes) > 0 {
			return dr.pageBuilder.addDropCapWithNotes(result, block.Notes)
		}
		return dr.pageBuilder.addDropCap(result)
	}

	if len(block.Notes) == 0 {
		return dr.pageBuilder.addDropCapParagraph(spans, overrides...)
	}

	result, err := dr.renderer.dropCapParagraph(spans, overrides...)
	if err != nil {
		return fmt.Errorf("renderDropCapParagraph: %w", err)
	}
	return dr.pageBuilder.addDropCapWithNotes(result, block.Notes)
}

// renderTitle renders a # heading.
func (dr *DocumentRenderer) renderTitle(block ir.Block) error {
	dr.pageBuilder.addHeadingSpace()

	// Record anchor before title.
	if dr.pendingAnchor != "" {
		dr.anchorPages[dr.pendingAnchor] = dr.pageBuilder.pageNum
		dr.pageBuilder.addDestinationAt(dr.pendingAnchor, dr.pageBuilder.currentY)
		dr.pendingAnchor = ""
	}

	if err := dr.pageBuilder.addTitle(block.Text); err != nil {
		return err
	}

	// Record anchor after title (may have triggered a page break).
	if dr.pendingAnchor != "" {
		dr.anchorPages[dr.pendingAnchor] = dr.pageBuilder.pageNum
		dr.pageBuilder.addDestinationAt(dr.pendingAnchor, dr.pageBuilder.contentTop)
		dr.pendingAnchor = ""
	}
	return nil
}

// renderHeading renders a ## heading.
func (dr *DocumentRenderer) renderHeading(block ir.Block) error {
	dr.pageBuilder.addHeadingSpace()

	dr.pageBuilder.setPendingCatchword(&pendingCatchword{
		text:  firstWord(block.Text),
		color: cu.Kinovar,
	})

	b, err := dr.renderer.sectionHeading(block.Text)
	if err != nil {
		return fmt.Errorf("renderHeading: %w", err)
	}

	// Record anchor before addHeading (need block dimensions for destination).
	if dr.pendingAnchor != "" {
		headingTop := dr.pageBuilder.currentY
		if dr.pageBuilder.headingFitsOnPage(b) {
			dr.anchorPages[dr.pendingAnchor] = dr.pageBuilder.pageNum
			dr.pageBuilder.addDestinationAt(dr.pendingAnchor, headingTop)
			dr.pendingAnchor = ""
		}
	}

	if err := dr.pageBuilder.addHeading(b); err != nil {
		return err
	}

	// Record anchor after heading is placed (may have triggered a page break).
	if dr.pendingAnchor != "" {
		dr.anchorPages[dr.pendingAnchor] = dr.pageBuilder.pageNum
		dr.pageBuilder.addDestinationAt(dr.pendingAnchor, dr.pageBuilder.contentTop)
		dr.pendingAnchor = ""
	}
	return nil
}

// renderImage renders an ornament block, which names the ornament.
func (dr *DocumentRenderer) renderImage(block ir.Block) error {
	dr.pageBuilder.addImageSpace()
	dr.pageBuilder.setPendingCatchword(nil) // ornaments have no text
	return dr.pageBuilder.addOrnament(block.Text, ornamentConfig{
		widthMM: 70,
	})
}

// ---- Catchword helpers ----

// firstWord extracts the first word from text (up to the first space).
// If the first word is 1-2 grapheme clusters, it also grabs subsequent words
// until the result exceeds 2 grapheme clusters.
func firstWord(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	result := words[0]
	for i := 1; i < len(words); i++ {
		if countGraphemes(result) > 2 {
			break
		}
		result += " " + words[i]
	}
	return result
}

// setCatchwordFromSpans sets a pending catchword from paragraph spans.
// Determines color based on rubric status of the first span.
func (dr *DocumentRenderer) setCatchwordFromSpans(spans []ir.Span) {
	// Concatenate all span texts
	var allText strings.Builder
	for _, s := range spans {
		allText.WriteString(s.Text)
	}
	word := firstWord(allText.String())
	if word == "" {
		dr.pageBuilder.setPendingCatchword(nil)
		return
	}

	// Find the first non-empty span to determine style
	var firstSpan *ir.Span
	for i := range spans {
		if spans[i].Text != "" {
			firstSpan = &spans[i]
			break
		}
	}
	if firstSpan == nil {
		dr.pageBuilder.setPendingCatchword(&pendingCatchword{text: word})
		return
	}

	if !firstSpan.Rubric {
		// Plain black
		dr.pageBuilder.setPendingCatchword(&pendingCatchword{text: word})
		return
	}

	// First span is rubric. Is the entire first word within this rubric span?
	wordGraphemes := countGraphemes(word)
	firstSpanGraphemes := countGraphemes(firstSpan.Text)

	if firstSpanGraphemes >= wordGraphemes {
		// Full first word is rubric → all red
		dr.pageBuilder.setPendingCatchword(&pendingCatchword{text: word, color: cu.Kinovar})
	} else {
		// Only part of the first word is rubric → ~ case: red first letter
		dr.pageBuilder.setPendingCatchword(&pendingCatchword{text: word, redFirstLetter: true})
	}
}

// setCatchwordFromDropCap sets a pending catchword for a drop cap paragraph.
// The catchword combines the drop cap letter with the first word of the body,
// always with red first letter.
func (dr *DocumentRenderer) setCatchwordFromDropCap(spans []ir.Span) {
	letter, bodySpans := splitSpansAtGrapheme(spans, 1)

	// Get first word from body spans
	var bodyText strings.Builder
	for _, s := range bodySpans {
		bodyText.WriteString(s.Text)
	}
	bodyWord := firstWord(bodyText.String())
	fullWord := letter + bodyWord

	if fullWord == "" {
		dr.pageBuilder.setPendingCatchword(nil)
		return
	}

	dr.pageBuilder.setPendingCatchword(&pendingCatchword{
		text:           fullWord,
		redFirstLetter: true,
	})
}

// setCatchwordForBlock sets a pending catchword from an IR block (for look-ahead).
func (dr *DocumentRenderer) setCatchwordForBlock(block *ir.Block) {
	switch block.Kind {
	case ir.BlockHeading1, ir.BlockHeading2:
		dr.pageBuilder.setPendingCatchword(&pendingCatchword{
			text:  firstWord(block.Text),
			color: cu.Kinovar,
		})
	case ir.BlockParagraph, ir.BlockCentered:
		dr.setCatchwordFromSpans(block.Content)
	case ir.BlockDropCap:
		dr.setCatchwordFromDropCap(block.Content)
	default:
		dr.pageBuilder.setPendingCatchword(nil)
	}
}

// findNextVisibleBlock returns the next block that produces visible content,
// skipping metadata blocks (RunningHeader, PageContextOp, ThematicBreak, Anchor).
func findNextVisibleBlock(blocks []ir.Block) *ir.Block {
	for i := range blocks {
		switch blocks[i].Kind {
		case ir.BlockRunningHeader, ir.BlockPageContextOp,
			ir.BlockThematicBreak, ir.BlockAnchor, ir.BlockPageBreak:
			continue
		default:
			return &blocks[i]
		}
	}
	return nil
}

// ---- Expression resolution ----

// hasExprSpans returns true if any span has a non-empty Expr field.
func hasExprSpans(spans []ir.Span) bool {
	for _, s := range spans {
		if s.Expr != "" {
			return true
		}
	}
	return false
}

// resolveSpans resolves all expression spans in a slice. An expression that
// fails is an error naming it, with the evaluator's reason. One that names
// an anchor not yet placed fails with an error wrapping
// expreval.ErrNeedsDefer: its paragraph is deferred, any other stops the
// render, since the variables are those the render began with and a
// mistake does not mend itself.
func (dr *DocumentRenderer) resolveSpans(spans []ir.Span) ([]ir.Span, error) {
	result := make([]ir.Span, len(spans))
	copy(result, spans)
	for i, s := range result {
		if s.Expr == "" {
			continue
		}
		resolved, err := expreval.Eval(s.Expr, dr.exprVars, dr.exprFuncs)
		if err != nil {
			return nil, fmt.Errorf("resolveSpans: ${%s}: %w", s.Expr, err)
		}
		result[i].Text = resolved
		result[i].Expr = ""
	}
	return result, nil
}

// resolveSpansBestEffort resolves expressions where possible, falling back to
// placeholder text for unresolvable ones.
func (dr *DocumentRenderer) resolveSpansBestEffort(spans []ir.Span) []ir.Span {
	result := make([]ir.Span, len(spans))
	copy(result, spans)
	for i, s := range result {
		if s.Expr == "" {
			continue
		}
		resolved, err := expreval.Eval(s.Expr, dr.exprVars, dr.exprFuncs)
		if err != nil {
			result[i].Text = "${" + s.Expr + "}"
		} else {
			result[i].Text = resolved
		}
		result[i].Expr = ""
	}
	return result
}

// deferBlock reserves space for a block with unresolved expressions and
// records it for later rendering. The block is a paragraph, plain, indented
// or centred (deferredParagraph); waiting is the error that sent it here,
// which names the expression.
//
// Its footnotes do not wait with it: their text is whole now (only the
// block names a page not yet made), so they are given their place at the
// foot of this page at once, as those of a block that is not deferred are.
// Its margin notes could only be lost, the room being filled in when its
// page is no longer the one notes are placed on: a block that has any is
// an error.
func (dr *DocumentRenderer) deferBlock(block ir.Block, waiting error) error {
	if len(block.Notes) > 0 {
		return fmt.Errorf("deferBlock1: a paragraph with a margin note cannot wait for a page not yet made: %w", waiting)
	}

	// Ensure a page exists.
	if dr.pageBuilder.pageNum == 0 {
		if err := dr.pageBuilder.breakPage(); err != nil {
			return err
		}
	}

	// Render with placeholder text to measure height.
	b, err := dr.renderer.renderParagraph(deferredParagraph(block, dr.resolveSpansBestEffort(block.Content)))
	if err != nil {
		return fmt.Errorf("deferBlock2: %w", err)
	}
	if b == nil {
		return nil
	}

	// The footnotes first: a page begun for them must be the page of the
	// block's room.
	footnotes, err := dr.preRenderFootnotes(block.Content)
	if err != nil {
		return err
	}
	if err := dr.reserveFootnotes(b.Height+b.Depth, footnotes); err != nil {
		return err
	}

	slot := dr.pageBuilder.reserveSpace(b.Height + b.Depth)
	dr.deferred = append(dr.deferred, deferredIR{block: block, slot: slot})
	return nil
}

// resolveAndRenderBlock resolves expressions in a deferred block and renders it.
func (dr *DocumentRenderer) resolveAndRenderBlock(block ir.Block) (*node.VList, error) {
	spans, err := dr.resolveSpans(block.Content)
	if err != nil {
		return nil, err
	}
	return dr.renderer.renderParagraph(deferredParagraph(block, spans))
}

// deferredParagraph is the paragraph a deferred block is set as, with spans
// for its content: the placeholder's when its room is measured (deferBlock),
// the resolved ones when the room is filled (resolveAndRenderBlock). One
// function for both, so that what is measured is what is set.
func deferredParagraph(block ir.Block, spans []ir.Span) ir.Paragraph {
	return ir.Paragraph{Content: spans, Centered: block.Kind == ir.BlockCentered, Indented: block.Indented, Minor: block.Minor}
}

// preRenderFootnotes renders footnote VLists for any footnote references in spans.
// Returns nil if there are no footnotes. A footnote's expressions are
// resolved as a paragraph's are, but a footnote is set with its page and
// cannot wait: one that names an anchor not yet placed is an error.
func (dr *DocumentRenderer) preRenderFootnotes(spans []ir.Span) ([]*node.VList, error) {
	if len(dr.footnoteByNum) == 0 {
		return nil, nil
	}

	var vlists []*node.VList
	for _, span := range spans {
		if span.FootnoteRef <= 0 {
			continue
		}
		fn, ok := dr.footnoteByNum[span.FootnoteRef]
		if !ok {
			continue // missing definition, warning already printed during parse
		}
		content, err := dr.resolveSpans(fn.Content)
		if err != nil {
			return nil, fmt.Errorf("preRenderFootnotes1: footnote %d: %w", fn.Num, err)
		}
		vl, err := dr.renderer.renderFootnote(fn.Num, content)
		if err != nil {
			return nil, fmt.Errorf("preRenderFootnotes2: footnote %d: %w", fn.Num, err)
		}
		vlists = append(vlists, vl)
	}

	return vlists, nil
}

// addBlockWithFootnotes adds a rendered block along with its footnotes,
// ensuring both land on the same page. If the combined height doesn't fit,
// a page break is triggered first so they move together.
func (dr *DocumentRenderer) addBlockWithFootnotes(b *node.VList, footnotes []*node.VList, notes []ir.MarginNote) error {
	if err := dr.reserveFootnotes(b.Height+b.Depth, footnotes); err != nil {
		return err
	}
	if len(notes) > 0 {
		return dr.pageBuilder.addWithNotes(b, notes)
	}
	return dr.pageBuilder.add(b)
}

// reserveFootnotes gives the footnotes of a block of that height their
// place at the foot of the block's page. Block and footnotes stay together:
// when they do not both fit, a new page is begun first. The block itself is
// added, or its room reserved, by the caller, after this.
func (dr *DocumentRenderer) reserveFootnotes(blockHeight bag.ScaledPoint, footnotes []*node.VList) error {
	if len(footnotes) == 0 {
		return nil
	}
	pb := dr.pageBuilder

	// Ensure a page exists.
	if pb.pageNum == 0 {
		if err := pb.breakPage(); err != nil {
			return err
		}
	}

	// Check if block + footnotes fit together on current page.
	if blockHeight+pb.footnoteSpaceNeeded(footnotes) > pb.availableHeight() {
		// Push both to the next page.
		if err := pb.breakPage(); err != nil {
			return err
		}
	}

	if !pb.registerFootnotes(footnotes) {
		// Still doesn't fit on a fresh page — register anyway.
		fmt.Fprintf(os.Stderr, "WARNING: footnotes don't fit on page\n")
	}
	return nil
}
