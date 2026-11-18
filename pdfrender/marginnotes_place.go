package pdfrender

import (
	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/grisha/slavonic-render/ir"
)

// addWithNotes adds a VList to the page, rendering and placing margin notes
// on the correct pages when content spans multiple pages. Notes are resolved
// against the block's line structure, page-1 notes are placed before the block
// is added, and overflow notes are placed via an overflow handler.
func (pb *pageBuilder) addWithNotes(block *node.VList, notes []ir.MarginNote) error {
	if len(notes) == 0 {
		return pb.add(block)
	}

	// addVList puts nothing on a page with less than one line of space
	// left and starts the block on the next page. The notes are placed
	// here, before the block is added, so they would stay behind on the
	// current page against text that moved. Start the next page first so
	// notes and text are measured from the same top.
	if pb.page == nil || pb.availableHeight() < pb.renderer.Leading {
		if err := pb.breakPage(); err != nil {
			return err
		}
	}

	resolved := resolveMarginNotes(block, notes, 0)
	baseY := pb.currentY
	available := pb.availableHeight()
	fitHeight := vlistFitHeight(block, available)
	splitBoundary := baseY - fitHeight + 1

	remaining, err := pb.placeMarginNotes(resolved, baseY, splitBoundary)
	if err != nil {
		return err
	}

	return pb.addBlockWithNotes(block, remaining)
}

// addHeadingWithNotes adds a heading VList with margin notes. If the heading
// fits on the current page, notes are placed first; if it's pushed to the
// next page, notes are placed after the heading on the new page.
func (pb *pageBuilder) addHeadingWithNotes(block *node.VList, notes []ir.MarginNote) error {
	if len(notes) == 0 {
		return pb.addHeading(block)
	}

	resolved := resolveMarginNotes(block, notes, 0)

	if pb.headingFitsOnPage(block) {
		baseY := pb.currentY
		pageBottom := pb.contentBottom
		if _, err := pb.placeMarginNotes(resolved, baseY, pageBottom); err != nil {
			return err
		}
		return pb.addHeading(block)
	}

	// Heading will be pushed to next page — add heading first,
	// then place notes on the new page alongside it.
	if err := pb.addHeading(block); err != nil {
		return err
	}
	baseY := pb.contentTop
	pageBottom := pb.contentBottom
	_, err := pb.placeMarginNotes(resolved, baseY, pageBottom)
	return err
}

// addDropCapWithNotes adds a drop cap result with margin notes, handling
// the three placement scenarios (fits, push, split) internally.
func (pb *pageBuilder) addDropCapWithNotes(result *dropCapResult, notes []ir.MarginNote) error {
	if len(notes) == 0 {
		return pb.addDropCap(result)
	}

	resolved := resolveMarginNotes(result.body, notes, len([]rune(result.letter)))
	baseY := pb.currentY
	pageBottom := pb.contentBottom

	fit := pb.predictDropCapFit(result)

	switch fit {
	case dropCapFitWhole:
		if _, err := pb.placeMarginNotes(resolved, baseY, pageBottom); err != nil {
			return err
		}
		return pb.addDropCap(result)

	case dropCapFitPushNext:
		if err := pb.addDropCap(result); err != nil {
			return err
		}
		newBaseY := pb.contentTop
		newBottom := pb.contentBottom
		_, err := pb.placeMarginNotes(resolved, newBaseY, newBottom)
		return err

	case dropCapFitSplit:
		available := baseY - pageBottom
		fitHeight := vlistFitHeight(result.body, available)
		splitBoundary := baseY - fitHeight + 1
		remaining, err := pb.placeMarginNotes(resolved, baseY, splitBoundary)
		if err != nil {
			return err
		}
		return pb.addSplitDropCap(result, remaining)
	}

	return nil
}

// placeMarginNotes renders and positions margin notes for the current page.
// Notes below pageContentBottom are returned as remaining (for overflow pages).
func (pb *pageBuilder) placeMarginNotes(resolved []resolvedMarginNote, baseY, pageContentBottom bag.ScaledPoint) (remaining []resolvedMarginNote, err error) {
	for _, rn := range resolved {
		y := baseY - rn.yOffset
		if y < pageContentBottom {
			remaining = append(remaining, rn)
			continue
		}

		noteBlock, err := pb.renderer.marginNote(rn.text)
		if err != nil {
			return nil, err
		}

		pb.placeInLeftMargin(noteBlock, y)
	}

	return remaining, nil
}

// placePageMarginNotes places margin notes that fall within a specific page's
// content range. noteIdx tracks progress through the notes slice.
func (pb *pageBuilder) placePageMarginNotes(notes []resolvedMarginNote, noteIdx *int, cumHeight, pageHeight bag.ScaledPoint) error {
	contentTop := pb.contentTop
	for *noteIdx < len(notes) {
		rn := notes[*noteIdx]
		if rn.yOffset >= cumHeight+pageHeight {
			break // belongs to a later page
		}
		y := contentTop - (rn.yOffset - cumHeight)
		noteBlock, err := pb.renderer.marginNote(rn.text)
		if err != nil {
			return err
		}
		pb.placeInLeftMargin(noteBlock, y)
		*noteIdx++
	}
	return nil
}

// addBlockWithNotes adds a block with overflow handling for remaining margin notes.
func (pb *pageBuilder) addBlockWithNotes(block *node.VList, remaining []resolvedMarginNote) error {
	if len(remaining) == 0 {
		return pb.add(block)
	}

	noteIdx := 0
	isFirstPage := true

	return pb.addWithOverflowHandler(block, func(cumHeight, pageHeight bag.ScaledPoint) error {
		if isFirstPage {
			isFirstPage = false
			return nil // page-1 notes already placed by caller
		}
		return pb.placePageMarginNotes(remaining, &noteIdx, cumHeight, pageHeight)
	})
}

// addSplitDropCap adds a drop cap result with overflow handling for remaining margin notes.
func (pb *pageBuilder) addSplitDropCap(result *dropCapResult, remaining []resolvedMarginNote) error {
	if len(remaining) == 0 {
		return pb.addDropCap(result)
	}

	noteIdx := 0

	return pb.addDropCapWithOverflowHandler(result, func(cumHeight, pageHeight bag.ScaledPoint) error {
		return pb.placePageMarginNotes(remaining, &noteIdx, cumHeight, pageHeight)
	})
}
