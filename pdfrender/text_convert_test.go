package pdfrender

import (
	"path/filepath"
	"testing"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/backend/node"
	"github.com/boxesandglue/boxesandglue/frontend"

	"github.com/grisha/slavonic-render/ir"
)

func TestRubricSize(t *testing.T) {
	cfg := textConfig{rubricSize: bag.MustSP("10pt")}
	size := func(p ir.Paragraph, i int) any {
		return paragraphToText(&p, cfg).Items[i].(*frontend.Text).Settings[frontend.SettingSize]
	}

	line := ir.Paragraph{Content: []ir.Span{
		{Text: "Ли́къ:", Rubric: true},
		{Text: " "},
		{Text: "Г", Rubric: true, Incipit: true},
		{Text: "дⷭ҇и поми́лꙋй."},
	}}
	if got := size(line, 0); got != cfg.rubricSize {
		t.Errorf("rubric label size = %v, want %v", got, cfg.rubricSize)
	}
	if got := size(line, 2); got != nil {
		t.Errorf("incipit size = %v, want the paragraph's (unset)", got)
	}

	// A paragraph that is all rubric (a |=...:= heading) is sized too.
	heading := ir.Paragraph{Content: []ir.Span{{Text: "вели́каѧ є҆ктенїѧ:", Rubric: true}}}
	if got := size(heading, 0); got != cfg.rubricSize {
		t.Errorf("rubric heading size = %v, want %v", got, cfg.rubricSize)
	}
}

func TestLargestSize(t *testing.T) {
	cfg := textConfig{rubricSize: bag.MustSP("12pt"), plainSize: bag.MustSP("11pt"), plainFont: &frontend.FontFamily{}}
	body := bag.MustSP("14pt")
	for _, c := range []struct {
		name  string
		spans []ir.Span
		want  bag.ScaledPoint
	}{
		{"all rubric, |=ѱало́мъ г҃:=", []ir.Span{{Text: "ѱало́мъ г҃:", Rubric: true}}, cfg.rubricSize},
		{"all plain", []ir.Span{{Text: "Священнослужители в алтаре:", Plain: true}}, cfg.plainSize},
		{"label and incipit", []ir.Span{{Text: "Ли́къ:", Rubric: true}, {Text: " "},
			{Text: "Г", Rubric: true, Incipit: true}, {Text: "дⷭ҇и поми́лꙋй."}}, body},
		{"plain with a rubric, the space between not counted", []ir.Span{{Text: "Поется стихира", Plain: true},
			{Text: " "}, {Text: "«Го́споди, воззва́хъ»", Rubric: true}, {Text: ", затем:", Plain: true}}, cfg.rubricSize},
		{"body text", []ir.Span{{Text: "Бл҃же́нъ мꙋ́жъ"}}, body},
		{"no text", []ir.Span{{Indent: true}}, 0},
	} {
		p := ir.Paragraph{Content: c.spans}
		if got := largestSize(&p, body, cfg); got != c.want {
			t.Errorf("%s: largestSize = %v, want %v", c.name, got, c.want)
		}
	}
}

// The leaders of a contents block end in one column whatever the width of
// the page number after them, and every line still fills the measure; in
// the column, Slavonic numerals are centred, Arabic set flush right.
func TestAlignLeaders(t *testing.T) {
	doc, err := frontend.New(filepath.Join(t.TempDir(), "leaders.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	fnts, err := loadFonts(doc, "Ponomar", "Indiction")
	if err != nil {
		t.Fatal(err)
	}
	r := newRenderer(doc, Config{fontFamilies: fnts, BodySize: bag.MustSP("14pt")})
	r.setTextWidth(bag.MustSP("180mm"))

	// line is where, on one line, the leader ends and the glyphs after it
	// begin and end.
	type line struct{ leaderEnd, from, to bag.ScaledPoint }
	render := func(numbers ...string) []line {
		t.Helper()
		var spans []ir.Span
		for i, num := range numbers {
			spans = append(spans,
				ir.Span{Text: "Каѳі́сма", Link: "c"},
				ir.Span{Leader: ".", Link: "c"},
				ir.Span{Text: num, Link: "c", Break: i < len(numbers)-1})
		}
		b, err := r.renderParagraph(ir.Paragraph{Content: spans, Indented: true})
		if err != nil {
			t.Fatal(err)
		}
		var lines []line
		for n := b.List; n != nil; n = n.Next() {
			hl, ok := n.(*node.HList)
			if !ok {
				continue
			}
			if w, _, _ := node.Dimensions(hl.List, nil, node.Horizontal); w != hl.Width {
				t.Errorf("line %d: contents %v wide, line %v", len(lines)+1, w, hl.Width)
			}
			var l line
			var x bag.ScaledPoint
			for e := hl.List; e != nil; e = e.Next() {
				w, _, _ := e.Sizes(node.Horizontal)
				if g, ok := e.(*node.Glue); ok && g.Leader != nil {
					l.leaderEnd = x + w
				}
				if _, ok := e.(*node.Glyph); ok && l.leaderEnd > 0 {
					if l.from == 0 {
						l.from = x
					}
					l.to = x + w
				}
				x += w
			}
			lines = append(lines, l)
		}
		if len(lines) != len(numbers) {
			t.Fatalf("got %d lines, want %d", len(lines), len(numbers))
		}
		for i, l := range lines[1:] {
			if l.leaderEnd != lines[0].leaderEnd {
				t.Errorf("%v: leader of line %d ends at %v, of line 1 at %v", numbers, i+2, l.leaderEnd, lines[0].leaderEnd)
			}
		}
		return lines
	}

	slavonic := render("д҃.", "н҃а.", "р҃г.")
	for i, l := range slavonic[1:] {
		// Centres in sp, to within the halving of an odd difference.
		if c, c0 := l.from+l.to, slavonic[0].from+slavonic[0].to; c-c0 > 1 || c0-c > 1 {
			t.Errorf("Slavonic: number of line %d centred at %v, of line 1 at %v", i+2, (l.from+l.to)/2, (slavonic[0].from+slavonic[0].to)/2)
		}
	}
	arabic := render("2.", "18.", "61.")
	for i, l := range arabic[1:] {
		if l.to != arabic[0].to {
			t.Errorf("Arabic: number of line %d ends at %v, of line 1 at %v", i+2, l.to, arabic[0].to)
		}
	}
}
