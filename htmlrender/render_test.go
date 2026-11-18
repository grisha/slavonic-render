package htmlrender

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/grisha/slavonic-render/cu"
	"github.com/grisha/slavonic-render/ir"
	"github.com/grisha/slavonic-render/markup"
)

func TestRender(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks []ir.Block
		want   string // what stands inside the <article>
	}{
		{
			"a paragraph",
			[]ir.Block{{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("Поми́лꙋй мѧ̀, бж҃е")}}},
			"<p>Поми́лꙋй мѧ̀, бж҃е</p>\n",
		},
		{
			"the flags of a paragraph, after its kind",
			[]ir.Block{
				{Kind: ir.BlockParagraph, Indented: true, Minor: true, Content: []ir.Span{ir.T("a")}},
				{Kind: ir.BlockCentered, Minor: true, Content: []ir.Span{ir.T("b")}},
				{Kind: ir.BlockDropCap, Content: []ir.Span{ir.T("c")}},
			},
			"<p class=\"indented minor\">a</p>\n" +
				"<p class=\"centered minor\">b</p>\n" +
				"<p class=\"dropcap\">c</p>\n",
		},
		{
			"headings and the blank line",
			[]ir.Block{
				{Kind: ir.BlockHeading1, Text: "Всено́щное бдѣ́нїе"},
				{Kind: ir.BlockThematicBreak},
				{Kind: ir.BlockHeading2, Text: "Гла́съ ѕ҃."},
			},
			"<h1>Всено́щное бдѣ́нїе</h1>\n<hr>\n<h2>Гла́съ ѕ҃.</h2>\n",
		},
		{
			"a label, an incipit, a plain instruction",
			[]ir.Block{{Kind: ir.BlockParagraph, Content: []ir.Span{
				ir.R("Ли́къ:"),
				ir.T(" "),
				{Text: "А҆", Rubric: true, Incipit: true},
				ir.T("ми́нь. "),
				{Text: "Трижды.", Plain: true},
			}}},
			`<p><span class="rubric">Ли́къ:</span> <span class="incipit">А҆</span>ми́нь. <span class="plain">Трижды.</span></p>` + "\n",
		},
		{
			"neighbouring spans of one class share a <span>",
			[]ir.Block{{Kind: ir.BlockCentered, Content: []ir.Span{ir.R("Прокі́менъ, "), ir.R("гла́съ ѕ҃:")}}},
			`<p class="centered"><span class="rubric">Прокі́менъ, гла́съ ѕ҃:</span></p>` + "\n",
		},
		{
			"a forced break and an inline indent",
			[]ir.Block{{Kind: ir.BlockParagraph, Content: []ir.Span{
				{Text: "Бл҃гослове́нъ є҆сѝ гдⷭ҇и,", Break: true},
				{Indent: true},
				ir.T("наꙋчи́ мѧ"),
			}}},
			"<p>Бл҃гослове́нъ є҆сѝ гдⷭ҇и,<br>\n<span class=\"indent\"></span>наꙋчи́ мѧ</p>\n",
		},
		{
			// Two lines of the contents. The leader and the page number
			// are not rendered; the break on the first line's last span
			// ends its link.
			"links: one <a> for the spans of one link",
			[]ir.Block{{Kind: ir.BlockParagraph, Indented: true, Content: []ir.Span{
				{Text: "А", Rubric: true, Incipit: true, Link: "paragraphs"},
				{Text: "бзацы", Link: "paragraphs"},
				{Leader: ".", Link: "paragraphs"},
				{Expr: `page("paragraphs")`, Link: "paragraphs"},
				{Text: ".", Link: "paragraphs", Break: true},
				{Text: "Примечания.", Link: "notes"},
				ir.T(" и҆ про́чее"),
			}}},
			`<p class="indented"><a href="#paragraphs"><span class="incipit">А</span>бзацы.</a><br>` + "\n" +
				`<a href="#notes">Примечания.</a> и҆ про́чее</p>` + "\n",
		},
		{
			"a break on a span that is not rendered",
			[]ir.Block{{Kind: ir.BlockParagraph, Content: []ir.Span{
				ir.T("помо́лимсѧ."),
				{Leader: ".", Break: true},
				ir.T("Гдⷭ҇и поми́лꙋй."),
			}}},
			"<p>помо́лимсѧ.<br>\nГдⷭ҇и поми́лꙋй.</p>\n",
		},
		{
			"an anchor is the id of the next element",
			[]ir.Block{
				{Kind: ir.BlockAnchor, Text: "notes"},
				{Kind: ir.BlockHeading2, Text: "Примечания."},
				{Kind: ir.BlockAnchor, Text: "end"},
				{Kind: ir.BlockCentered, Content: []ir.Span{ir.T("Коне́цъ.")}},
			},
			"<h2 id=\"notes\">Примечания.</h2>\n<p class=\"centered\" id=\"end\">Коне́цъ.</p>\n",
		},
		{
			"what belongs to the page is left out",
			[]ir.Block{
				{Kind: ir.BlockRunningHeader, Text: "Недѣ́лѧ"},
				{Kind: ir.BlockPageContextOp, Key: "tone", Value: "6"},
				{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("a")}},
			},
			"<p>a</p>\n",
		},
		{
			// The anchor before the break is the id of the heading after it.
			"a page break is a gap, and takes no anchor",
			[]ir.Block{
				{Kind: ir.BlockParagraph, Content: []ir.Span{ir.T("a")}},
				{Kind: ir.BlockAnchor, Text: "matins"},
				{Kind: ir.BlockPageBreak},
				{Kind: ir.BlockHeading2, Text: "Оу҆́тренѧ"},
			},
			"<p>a</p>\n<hr class=\"page\">\n<h2 id=\"matins\">Оу҆́тренѧ</h2>\n",
		},
		{
			// The first note after the indent and before the incipit; the
			// second inside a span, at its rune and not at its byte.
			"a margin note stands in the text, before its word",
			[]ir.Block{{Kind: ir.BlockParagraph,
				Content: []ir.Span{
					{Indent: true},
					{Text: "Г", Rubric: true, Incipit: true},
					ir.T("дⷭ҇ь пасе́тъ. На мѣ́стѣ"),
				},
				Notes: []ir.MarginNote{
					{CharPos: 0, Text: "сⷯ а҃"},
					{CharPos: runes("Гдⷭ҇ь пасе́тъ. "), Text: "в҃"},
				},
			}},
			`<p><span class="indent"></span><span class="note">сⷯ а҃&nbsp;</span><span class="incipit">Г</span>` +
				`дⷭ҇ь пасе́тъ. <span class="note">в҃&nbsp;</span>На мѣ́стѣ</p>` + "\n",
		},
		{
			"a note inside a rubric closes it and opens it again",
			[]ir.Block{{Kind: ir.BlockParagraph,
				Content: []ir.Span{ir.R("Сла́ва, и҆ ны́нѣ")},
				Notes:   []ir.MarginNote{{CharPos: runes("Сла́ва, "), Text: "в҃"}},
			}},
			`<p><span class="rubric">Сла́ва, </span><span class="note">в҃&nbsp;</span><span class="rubric">и҆ ны́нѣ</span></p>` + "\n",
		},
		{
			// The first is where one span ends and the next begins, after
			// the forced break; the second after the last of the text.
			"notes between two spans and at the end; a note is escaped",
			[]ir.Block{{Kind: ir.BlockParagraph,
				Content: []ir.Span{{Text: "a", Break: true}, ir.T("b")},
				Notes:   []ir.MarginNote{{CharPos: 1, Text: "1 < 2"}, {CharPos: 2, Text: "y"}},
			}},
			"<p>a<br>\n" + `<span class="note">1 &lt; 2&nbsp;</span>b<span class="note">y&nbsp;</span></p>` + "\n",
		},
		{
			// Only the note before the drop cap is marked, and only there:
			// the first case's note at 0 stands in a plain paragraph.
			"the note before a drop cap is marked for the style sheet",
			[]ir.Block{{Kind: ir.BlockDropCap,
				Content: []ir.Span{ir.T("Бл҃же́нъ мꙋ́жъ. Но")},
				Notes: []ir.MarginNote{
					{CharPos: 0, Text: "сⷯ а҃"},
					{CharPos: runes("Бл҃же́нъ мꙋ́жъ. "), Text: "в҃"},
				},
			}},
			`<p class="dropcap"><span class="note lead">сⷯ а҃&nbsp;</span>Бл҃же́нъ мꙋ́жъ. <span class="note">в҃&nbsp;</span>Но</p>` + "\n",
		},
		{
			"text is escaped, in an attribute too",
			[]ir.Block{
				{Kind: ir.BlockAnchor, Text: `a"b`},
				{Kind: ir.BlockHeading2, Text: "1 < 2 & 3"},
				{Kind: ir.BlockParagraph, Content: []ir.Span{{Text: "<b>", Link: `c"d`}}},
			},
			"<h2 id=\"a&#34;b\">1 &lt; 2 &amp; 3</h2>\n<p><a href=\"#c&#34;d\">&lt;b&gt;</a></p>\n",
		},
	} {
		var got strings.Builder
		if err := Render(&got, &ir.Document{Blocks: tc.blocks}, nil); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		want := "<article class=\"slavonic\">\n" + tc.want + "</article>\n"
		if got.String() != want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got.String(), want)
		}
	}
}

// runes counts as a margin note's CharPos does.
func runes(s string) int { return len([]rune(s)) }

// The parser's count of a note's place and the renderer's are one count:
// markup with two notes comes out with each before its own word.
func TestNotesFromMarkup(t *testing.T) {
	doc, err := markup.Parse("-~&{margin сⷯ а҃}Гдⷭ҇ь пасе́тъ. &{margin в҃}На мѣ́стѣ")
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	if err := Render(&got, doc, nil); err != nil {
		t.Fatal(err)
	}
	want := "<article class=\"slavonic\">\n" +
		`<p><span class="indent"></span><span class="note">сⷯ а҃&nbsp;</span><span class="incipit">Г</span>` +
		`дⷭ҇ь пасе́тъ. <span class="note">в҃&nbsp;</span>На мѣ́стѣ</p>` + "\n</article>\n"
	if got.String() != want {
		t.Errorf("\n got %q\nwant %q", got.String(), want)
	}
}

// An expression is evaluated as the PDF renderer evaluates it, with the
// front matter's keys for its names, and written in its span's style. HTML
// has no pages, so page() is empty.
func TestExpressions(t *testing.T) {
	frontMatter := map[string]string{"Feast": "Pascha", "Tone": "6"}
	for _, tc := range []struct {
		name    string
		content []ir.Span
		want    string
	}{
		{
			"a key of the front matter",
			[]ir.Span{ir.T("Today is "), {Expr: "Feast"}, ir.T(".")},
			"<p>Today is Pascha.</p>\n",
		},
		{
			"a function, arithmetic on a key, and no key at all",
			[]ir.Span{{Expr: "upper(Feast)"}, ir.T(" "), {Expr: "int(Tone) + 1"}, ir.T(" "), {Expr: "1 + 1"}},
			"<p>PASCHA 7 2</p>\n",
		},
		{
			"in a rubric: one <span> with the text around it",
			[]ir.Span{{Text: "Tone ", Rubric: true}, {Expr: "Tone", Rubric: true}, {Text: ".", Rubric: true}},
			`<p><span class="rubric">Tone 6.</span></p>` + "\n",
		},
		{
			"in a link: inside its <a>",
			[]ir.Span{{Text: "See ", Link: "c1"}, {Expr: "Feast", Link: "c1"}},
			`<p><a href="#c1">See Pascha</a></p>` + "\n",
		},
		{
			"page() is empty",
			[]ir.Span{ir.T("Chapter one is on page "), {Expr: `page("c1")`}, ir.T(".")},
			"<p>Chapter one is on page .</p>\n",
		},
		{
			"page() alone writes nothing, not an empty <span>",
			[]ir.Span{{Expr: `page("c1")`, Rubric: true}},
			"<p></p>\n",
		},
		{
			"what is written is escaped",
			[]ir.Span{{Expr: `"<" + Feast + ">"`}},
			"<p>&lt;Pascha&gt;</p>\n",
		},
	} {
		var got strings.Builder
		doc := &ir.Document{Blocks: []ir.Block{{Kind: ir.BlockParagraph, Content: tc.content}}}
		if err := Render(&got, doc, frontMatter); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		want := "<article class=\"slavonic\">\n" + tc.want + "</article>\n"
		if got.String() != want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got.String(), want)
		}
	}
}

// What an expression gives is not text the parser counted, so the margin
// notes of its paragraph stand where they stood: one written before the
// expression comes before its value, and one further on before its own
// word.
func TestExpressionsFromMarkup(t *testing.T) {
	doc, err := markup.Parse("=Tone ${Tone}.= &{margin в҃}${upper(Feast)} бб &{margin г҃}вв, ${page(\"c1\")}.")
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	if err := Render(&got, doc, map[string]string{"Feast": "Pascha", "Tone": "6"}); err != nil {
		t.Fatal(err)
	}
	want := "<article class=\"slavonic\">\n" +
		`<p><span class="rubric">Tone 6.</span> <span class="note">в҃&nbsp;</span>PASCHA бб ` +
		`<span class="note">г҃&nbsp;</span>вв, .</p>` + "\n</article>\n"
	if got.String() != want {
		t.Errorf("\n got %q\nwant %q", got.String(), want)
	}
}

// An expression that fails is an error naming it, as it is for the PDF
// renderer: a name that is not a key does not pass as an empty text.
func TestExpressionUnknownName(t *testing.T) {
	var out strings.Builder
	doc := &ir.Document{Blocks: []ir.Block{{Kind: ir.BlockParagraph, Content: []ir.Span{{Expr: "Feest"}}}}}
	err := Render(&out, doc, map[string]string{"Feast": "Pascha"})
	if err == nil || !strings.HasPrefix(err.Error(), "Render: ") || !strings.Contains(err.Error(), "${Feest}") {
		t.Errorf("got %v, want an error of Render naming the expression", err)
	}
}

// A footnote reference is a raised number that links to its note, outside
// the <span> of the rubric it stands in; the notes come after the text, each
// opening with its number, which links back. The second reference to note 1
// has no id: the way back from the note is to the first.
func TestFootnotes(t *testing.T) {
	doc := &ir.Document{
		Blocks: []ir.Block{
			{Kind: ir.BlockCentered, Content: []ir.Span{
				ir.R("Каѳі́сма:"),
				{FootnoteRef: 1},
				ir.R(" и҆ про́чее"),
			}},
			{Kind: ir.BlockParagraph, Content: []ir.Span{
				ir.T("помо́лимсѧ."),
				{FootnoteRef: 2, Break: true},
				ir.T("Гдⷭ҇и поми́лꙋй."),
				{FootnoteRef: 1},
			}},
		},
		Footnotes: []ir.FootnoteDef{
			{ID: "kathisma", Num: 1, Content: []ir.Span{ir.T("Вместо 2-й & 3-й кафизм: "), ir.R("Бл҃же́нъ мꙋ́жъ.")}},
			{ID: "peace", Num: 2, Content: []ir.Span{ir.T("По изданию.")}},
		},
	}
	var got strings.Builder
	if err := Render(&got, doc, nil); err != nil {
		t.Fatal(err)
	}
	want := "<article class=\"slavonic\">\n" +
		`<p class="centered"><span class="rubric">Каѳі́сма:</span>` +
		`<sup id="footnote-ref-1"><a href="#footnote-1">1</a></sup>` +
		`<span class="rubric"> и҆ про́чее</span></p>` + "\n" +
		`<p>помо́лимсѧ.<sup id="footnote-ref-2"><a href="#footnote-2">2</a></sup><br>` + "\n" +
		`Гдⷭ҇и поми́лꙋй.<sup><a href="#footnote-1">1</a></sup></p>` + "\n" +
		"<aside class=\"footnotes\">\n" +
		`<p id="footnote-1"><a href="#footnote-ref-1">1.</a> Вместо 2-й &amp; 3-й кафизм: <span class="rubric">Бл҃же́нъ мꙋ́жъ.</span></p>` + "\n" +
		`<p id="footnote-2"><a href="#footnote-ref-2">2.</a> По изданию.</p>` + "\n" +
		"</aside>\n</article>\n"
	if got.String() != want {
		t.Errorf("\n got %q\nwant %q", got.String(), want)
	}
}

// The parser's numbers and the renderer's links are one numbering: the notes
// are numbered by their references, not by where their definitions stand.
func TestFootnotesFromMarkup(t *testing.T) {
	doc, err := markup.Parse("Пе́рвое.[^b] Второ́е.[^a]\n\n[^a]: Вторая.\n\n[^b]: Первая.\n")
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	if err := Render(&got, doc, nil); err != nil {
		t.Fatal(err)
	}
	want := "<article class=\"slavonic\">\n" +
		`<p>Пе́рвое.<sup id="footnote-ref-1"><a href="#footnote-1">1</a></sup>` +
		` Второ́е.<sup id="footnote-ref-2"><a href="#footnote-2">2</a></sup></p>` + "\n" +
		"<aside class=\"footnotes\">\n" +
		`<p id="footnote-1"><a href="#footnote-ref-1">1.</a> Первая.</p>` + "\n" +
		`<p id="footnote-2"><a href="#footnote-ref-2">2.</a> Вторая.</p>` + "\n" +
		"</aside>\n</article>\n"
	if got.String() != want {
		t.Errorf("\n got %q\nwant %q", got.String(), want)
	}
}

// failingWriter refuses every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// The writer's error comes back, with the function's name on it.
func TestRenderWriteError(t *testing.T) {
	err := Render(failingWriter{}, &ir.Document{}, nil)
	if err == nil || err.Error() != "Render: disk full" {
		t.Errorf("got %v, want the writer's error", err)
	}
}

// An ornament is its SVG source as an element of the page: from the <svg
// tag on, marked for the style sheet and hidden from a screen reader, its
// black turned into currentColor. It takes no anchor: the anchor before it
// is the id of the element after it. Every file of internal/assets/svg is
// tried, so that a new ornament of another form is caught here.
func TestOrnament(t *testing.T) {
	files, err := os.ReadDir("../internal/assets/svg")
	if err != nil {
		t.Fatal(err)
	}
	ornaments := 0
	for _, f := range files {
		name, ok := strings.CutPrefix(f.Name(), "ornament-")
		if ok {
			name, ok = strings.CutSuffix(name, ".svg")
		}
		if !ok {
			continue // the README
		}
		ornaments++
		var out strings.Builder
		err := Render(&out, &ir.Document{Blocks: []ir.Block{
			{Kind: ir.BlockAnchor, Text: "end"},
			{Kind: ir.BlockImage, Text: name},
			{Kind: ir.BlockHeading2, Text: "Коне́цъ."},
		}}, nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := out.String()
		if want := "<article class=\"slavonic\">\n<svg class=\"ornament\" aria-hidden=\"true\" version="; !strings.HasPrefix(got, want) {
			t.Errorf("%s: begins %.90q, want %q", name, got, want)
		}
		if want := "</g>\n</svg>\n<h2 id=\"end\">Коне́цъ.</h2>\n</article>\n"; !strings.HasSuffix(got, want) {
			t.Errorf("%s: ends %q, want %q", name, got[len(got)-80:], want)
		}
		for part, want := range map[string]int{
			"<svg": 1, "</svg>": 1, "viewBox=\"": 1, `fill="currentColor"`: 1,
			"<?xml": 0, "<!DOCTYPE": 0, "#000000": 0,
		} {
			if n := strings.Count(got, part); n != want {
				t.Errorf("%s: %q is there %d times, want %d", name, part, n, want)
			}
		}
	}
	if ornaments == 0 {
		t.Error("no ornament in ../internal/assets/svg")
	}
}

// An ornament that does not exist is an error naming it, as it is for the
// PDF renderer.
func TestOrnamentUnknown(t *testing.T) {
	var out strings.Builder
	err := Render(&out, &ir.Document{Blocks: []ir.Block{{Kind: ir.BlockImage, Text: "nosuch"}}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "Render: ") || !strings.Contains(err.Error(), `"nosuch"`) {
		t.Errorf("got %v, want an error of Render naming the ornament", err)
	}
}

// The style sheet's kinovar is the PDF's on a light page, in both places
// it is set: alone, and as the light half of light-dark().
func TestCSSKinovar(t *testing.T) {
	for _, decl := range []string{
		"--kinovar: " + cu.Kinovar + ";",
		"--kinovar: light-dark(" + cu.Kinovar + ", ",
	} {
		if !strings.Contains(CSS, decl) {
			t.Errorf("the style sheet lacks %q (cu.Kinovar)", decl)
		}
	}
}
