package pdfrender

import (
	"slices"

	"github.com/boxesandglue/boxesandglue/backend/node"
)

// glyphText rebuilds source text from the glyphs of a line.
//
// Glyph.Components holds the source runes behind a glyph, but a base letter
// with combining marks is one shaping cluster set with several glyphs, and
// boxesandglue spreads the cluster's runes over those glyphs with overlap:
// since v0.2.61 the base glyph carries "а" and the mark glyph "а́", earlier
// versions gave "" and "а́". Concatenating doubles the letter. So glyphs are
// grouped into clusters (a spacing glyph plus the mark glyphs after it) and
// each glyph's components are merged into its cluster minus the overlap.
type glyphText struct {
	runes   []rune
	cluster int // index in runes where the current cluster starts
}

// add merges a glyph's components into the text.
func (t *glyphText) add(g *node.Glyph) {
	if !isMarkGlyph(g) {
		t.cluster = len(t.runes)
	}
	acc := t.runes[t.cluster:]
	comp := []rune(g.Components)
	k := min(len(acc), len(comp))
	for k > 0 && !slices.Equal(acc[len(acc)-k:], comp[:k]) {
		k--
	}
	t.runes = append(t.runes, comp[k:]...)
}

// addSpace appends an inter-word space.
func (t *glyphText) addSpace() {
	t.runes = append(t.runes, ' ')
	t.cluster = len(t.runes)
}

func (t *glyphText) String() string { return string(t.runes) }

// isMarkGlyph reports whether g is a combining mark, judged by its advance:
// marks in the Ponomar fonts advance by 0 or 1 font unit, while the
// narrowest letter is about a quarter em.
func isMarkGlyph(g *node.Glyph) bool {
	if g.Font == nil {
		return g.Width == 0
	}
	return g.Width < g.Font.Size/20
}
