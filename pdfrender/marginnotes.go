package pdfrender

import (
	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/node"

	"github.com/grisha/slavonic-render/ir"
)

// marginNoteGap is the white between a margin note and the text.
var marginNoteGap = bag.MustSP("5pt")

// resolvedMarginNote holds a pre-computed margin note with its Y offset
// relative to the top of the VList.
type resolvedMarginNote struct {
	yOffset bag.ScaledPoint // distance from VList top to line baseline
	text    string
}

// resolveMarginNotes maps margin notes to their Y positions in a VList.
// This must be called BEFORE the VList is passed to pageBuilder.add(),
// because add() may split the VList and detach nodes.
func resolveMarginNotes(vlist *node.VList, notes []ir.MarginNote, charOffset int) []resolvedMarginNote {
	if len(notes) == 0 || vlist == nil {
		return nil
	}

	lineNotes := mapNotesToLines(vlist, notes, charOffset)
	if len(lineNotes) == 0 {
		return nil
	}

	merged := mergeLineNotes(lineNotes)
	lineYOffsets := computeLineYOffsets(vlist)

	var result []resolvedMarginNote
	for _, ln := range merged {
		if ln.lineIndex >= len(lineYOffsets) {
			continue
		}
		result = append(result, resolvedMarginNote{
			yOffset: lineYOffsets[ln.lineIndex],
			text:    ln.text,
		})
	}
	return result
}

// vlistFitHeight computes the height of content that would fit within maxHeight,
// splitting at line boundaries (matching splitVList logic). This is used to
// determine the correct page boundary for margin note placement.
func vlistFitHeight(b *node.VList, maxHeight bag.ScaledPoint) bag.ScaledPoint {
	if b == nil {
		return 0
	}
	var h bag.ScaledPoint
	for n := b.List; n != nil; n = n.Next() {
		var nh bag.ScaledPoint
		if hlist, ok := n.(*node.HList); ok {
			nh = hlist.Height + hlist.Depth
		} else if glue, ok := n.(*node.Glue); ok {
			nh = glue.Width
		} else if kern, ok := n.(*node.Kern); ok {
			nh = kern.Kern
		}
		if h+nh > maxHeight {
			break
		}
		h += nh
	}
	return h
}

// ---- internal helpers ----

// lineNote associates a margin note with a specific line in a VList.
type lineNote struct {
	lineIndex int    // which line (0-based) this note belongs to
	text      string // the margin note text
}

// isInterWordGlue returns true if the glue node represents a real inter-word
// space from the input text (as opposed to synthetic glue inserted by the
// line-breaker for indentation or PDF copy/paste).
func isInterWordGlue(glue *node.Glue) bool {
	if glue.Subtype != node.GlueDefault {
		return false
	}
	if glue.Attributes != nil {
		if origin, ok := glue.Attributes["origin"]; ok {
			switch origin {
			case "leftskip", "lineend":
				return false
			}
		}
	}
	return true
}

// mapNotesToLines walks a VList line by line, counting characters through
// glyph.Components, and determines which line each MarginNote falls on.
func mapNotesToLines(vlist *node.VList, notes []ir.MarginNote, charOffset int) []lineNote {
	if len(notes) == 0 || vlist == nil {
		return nil
	}

	var result []lineNote
	charCount := charOffset
	noteIdx := 0
	lineIdx := 0

	for n := vlist.List; n != nil; n = n.Next() {
		hlist, ok := n.(*node.HList)
		if !ok {
			continue
		}

		// Check if any notes fall on this line before we start counting
		// (notes at charPos == charCount, i.e., at the very start of this line)
		for noteIdx < len(notes) && notes[noteIdx].CharPos <= charCount {
			result = append(result, lineNote{
				lineIndex: lineIdx,
				text:      notes[noteIdx].Text,
			})
			noteIdx++
		}

		// Count characters in this line
		var line glyphText
		for hn := hlist.List; hn != nil; hn = hn.Next() {
			if glyph, ok := hn.(*node.Glyph); ok {
				line.add(glyph)
			} else if glue, ok := hn.(*node.Glue); ok {
				if isInterWordGlue(glue) {
					line.addSpace()
				}
			}
		}
		charCount += len(line.runes)

		// Check notes that fall within this line (strictly before the next line boundary)
		for noteIdx < len(notes) && notes[noteIdx].CharPos < charCount {
			result = append(result, lineNote{
				lineIndex: lineIdx,
				text:      notes[noteIdx].Text,
			})
			noteIdx++
		}

		// Account for the space consumed by the line break. The line breaker
		// discards the inter-word glue at each break point, so that space is
		// present in the source text's CharPos count but absent from the
		// VList nodes. Add 1 if this line was broken at a space (not a
		// hyphen) and there are more lines after it.
		if !lineEndsWithHyphen(hlist) && hasMoreLines(n.Next()) {
			charCount++
		}

		lineIdx++
	}

	// Any remaining notes go on the last line
	for noteIdx < len(notes) {
		result = append(result, lineNote{
			lineIndex: lineIdx - 1,
			text:      notes[noteIdx].Text,
		})
		noteIdx++
	}

	return result
}

// mergeLineNotes combines notes that share the same line index,
// joining their texts with a space.
func mergeLineNotes(notes []lineNote) []lineNote {
	if len(notes) == 0 {
		return nil
	}

	var result []lineNote
	current := notes[0]

	for i := 1; i < len(notes); i++ {
		if notes[i].lineIndex == current.lineIndex {
			current.text += " " + notes[i].text
		} else {
			result = append(result, current)
			current = notes[i]
		}
	}
	result = append(result, current)
	return result
}

// hasMoreLines returns true if there is at least one more HList node in the
// remaining VList chain (i.e., more lines follow the current one).
func hasMoreLines(n node.Node) bool {
	for ; n != nil; n = n.Next() {
		if _, ok := n.(*node.HList); ok {
			return true
		}
	}
	return false
}

// computeLineYOffsets returns the Y offset (from the top of the VList) of each
// HList (line) in the VList. The offset is measured to the baseline of each line.
func computeLineYOffsets(vlist *node.VList) []bag.ScaledPoint {
	var offsets []bag.ScaledPoint
	var y bag.ScaledPoint

	for n := vlist.List; n != nil; n = n.Next() {
		if hlist, ok := n.(*node.HList); ok {
			offsets = append(offsets, y)
			y += hlist.Height + hlist.Depth
		} else if glue, ok := n.(*node.Glue); ok {
			y += glue.Width
		} else if kern, ok := n.(*node.Kern); ok {
			y += kern.Kern
		}
	}

	return offsets
}
