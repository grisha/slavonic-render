package pdfrender

import (
	"path/filepath"
	"testing"

	"github.com/grisha/slavonic-render/fonts"
	"github.com/grisha/slavonic-render/ir"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/boxesandglue/boxesandglue/frontend"
)

func TestGlyphTextMergesClusters(t *testing.T) {
	mark := func(comp string) *node.Glyph { return &node.Glyph{Components: comp} }
	base := func(comp string) *node.Glyph { return &node.Glyph{Components: comp, Width: bag.MustSP("5pt")} }
	tests := []struct {
		name   string
		glyphs []*node.Glyph
		want   string
	}{
		{"boxesandglue v0.2.61: base carries its rune, mark the cluster",
			[]*node.Glyph{base("В"), base("а"), base("а"), mark("а́"), base("л")}, "Ваа́л"},
		{"boxesandglue v0.2.6: base empty, mark the cluster",
			[]*node.Glyph{base(""), mark("а́")}, "а́"},
		{"one rune per glyph",
			[]*node.Glyph{base("а"), mark("́")}, "а́"},
		{"middle mark carries the first rune only",
			[]*node.Glyph{base("г"), base("д"), mark("д"), mark("дⷭ҇"), base("ꙋ")}, "гдⷭ҇ꙋ"},
		{"mark glyph carries the preceding base too",
			[]*node.Glyph{base("ᲂ"), mark("ᲂу҆̀")}, "ᲂу҆̀"},
		{"precomposed single glyph",
			[]*node.Glyph{base("ѿ")}, "ѿ"},
	}
	for _, tt := range tests {
		var gt glyphText
		for _, g := range tt.glyphs {
			gt.add(g)
		}
		if got := gt.String(); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// shapePonomar sets text in Ponomar at 12pt into lines of the given width.
func shapePonomar(t *testing.T, text string, width bag.ScaledPoint) *node.VList {
	t.Helper()
	doc, err := frontend.New(filepath.Join(t.TempDir(), "shape.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := fonts.ReadData("Ponomar")
	if err != nil {
		t.Fatal(err)
	}
	ff := doc.NewFontFamily("text")
	if err := ff.AddMember(&frontend.FontSource{Data: data}, frontend.FontWeight400, frontend.FontStyleNormal); err != nil {
		t.Fatal(err)
	}
	para := &frontend.Text{
		Settings: frontend.TypesettingSettings{frontend.SettingFontFamily: ff, frontend.SettingSize: bag.MustSP("12pt")},
		Items:    []any{text},
	}
	vl, _, err := doc.FormatParagraph(para, width)
	if err != nil {
		t.Fatal(err)
	}
	return vl
}

func TestExtractFirstWordTextWithMarks(t *testing.T) {
	vl := shapePonomar(t, "А҆ллилꙋа́їа Ваа́лъ гдⷭ҇ꙋ", bag.MustSP("200mm"))
	if got, want := extractFirstWordText(vl), "А҆ллилꙋа́їа"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMapNotesToLinesWithMarks(t *testing.T) {
	text := "А҆ллилꙋа́їа Ваа́лъ гдⷭ҇ꙋ бж҃е ѿ є҆́й и҆̀ ᲂу҆̀ ѻ҆́ ѡ҆́ їи҃съ"
	vl := shapePonomar(t, text, bag.MustSP("30mm"))
	lines := 0
	for n := vl.List; n != nil; n = n.Next() {
		if _, ok := n.(*node.HList); ok {
			lines++
		}
	}
	if lines < 3 {
		t.Fatalf("expected several lines, got %d", lines)
	}
	// A note at the start of the last word belongs on the last line.
	pos := len([]rune(text)) - len([]rune("їи҃съ"))
	got := mapNotesToLines(vl, []ir.MarginNote{{CharPos: pos, Text: "n"}}, 0)
	if len(got) != 1 || got[0].lineIndex != lines-1 {
		t.Errorf("note placed at %+v, want line %d", got, lines-1)
	}
}
