package pdfrender

import (
	"reflect"
	"testing"

	"github.com/grisha/slavonic-render/ir"
)

func TestNormalizeDropCapDigraphs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "capital O + u with combining psili",
			input: "\u041E\u0443\u0486слы́ши",
			want:  "\uA64Aслы́ши",
		},
		{
			name:  "narrow o + u with combining psili",
			input: "\u1C82\u0443\u0486слы́ши",
			want:  "\uA64Aслы́ши",
		},
		{
			name:  "capital O + u without combining marks",
			input: "\u041E\u0443слы́ши",
			want:  "\uA64Aслы́ши",
		},
		{
			name:  "non-digraph unchanged",
			input: "Бж҃е, бж҃е мо́й",
			want:  "Бж҃е, бж҃е мо́й",
		},
		{
			name:  "single character unchanged",
			input: "Б",
			want:  "Б",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "О not followed by у",
			input: "\u041Eн же речѐ",
			want:  "\u041Eн же речѐ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDropCapDigraphs(tt.input)
			if got != tt.want {
				t.Errorf("normalizeDropCapDigraphs(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitSpansAtGrapheme(t *testing.T) {
	tests := []struct {
		name       string
		spans      []ir.Span
		count      int
		wantLetter string
		wantBody   []ir.Span
	}{
		{
			name:       "single plain span",
			spans:      []ir.Span{{Text: "Бж҃е мо́й"}},
			count:      1,
			wantLetter: "Б",
			wantBody:   []ir.Span{{Text: "ж҃е мо́й"}},
		},
		{
			name:       "split mid-span preserves rubric flag",
			spans:      []ir.Span{{Text: "Бж҃е мо́й", Rubric: true}},
			count:      1,
			wantLetter: "Б",
			wantBody:   []ir.Span{{Text: "ж҃е мо́й", Rubric: true}},
		},
		{
			name: "rubric first span consumed, body starts with plain",
			spans: []ir.Span{
				{Text: "Б", Rubric: true},
				{Text: "ж҃е мо́й"},
			},
			count:      1,
			wantLetter: "Б",
			wantBody:   []ir.Span{{Text: "ж҃е мо́й"}},
		},
		{
			name: "multi-span: letter spans two spans",
			spans: []ir.Span{
				{Text: "А"},
				{Text: "Б", Rubric: true},
				{Text: "вгд"},
			},
			count:      2,
			wantLetter: "АБ",
			wantBody:   []ir.Span{{Text: "вгд"}},
		},
		{
			name: "indent spans passed through to body",
			spans: []ir.Span{
				{Indent: true},
				{Text: "Бж҃е мо́й"},
			},
			count:      1,
			wantLetter: "Б",
			wantBody: []ir.Span{
				{Indent: true},
				{Text: "ж҃е мо́й"},
			},
		},
		{
			name:       "count exceeds span length",
			spans:      []ir.Span{{Text: "АБ"}},
			count:      5,
			wantLetter: "АБ",
			wantBody:   nil,
		},
		{
			name:       "empty spans",
			spans:      []ir.Span{},
			count:      1,
			wantLetter: "",
			wantBody:   nil,
		},
		{
			name: "grapheme cluster with combining mark",
			spans: []ir.Span{
				{Text: "А\u0301Бв"}, // А + combining accent
			},
			count:      1,
			wantLetter: "А\u0301",
			wantBody:   []ir.Span{{Text: "Бв"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLetter, gotBody := splitSpansAtGrapheme(tt.spans, tt.count)
			if gotLetter != tt.wantLetter {
				t.Errorf("letter = %q, want %q", gotLetter, tt.wantLetter)
			}
			if !reflect.DeepEqual(gotBody, tt.wantBody) {
				t.Errorf("body = %v, want %v", gotBody, tt.wantBody)
			}
		})
	}
}

func TestNormalizeFirstSpanDigraphs(t *testing.T) {
	tests := []struct {
		name  string
		spans []ir.Span
		want  []ir.Span
	}{
		{
			name:  "digraph in first span",
			spans: []ir.Span{{Text: "\u041E\u0443слы́ши"}},
			want:  []ir.Span{{Text: "\uA64Aслы́ши"}},
		},
		{
			name: "skips indent span to find first text",
			spans: []ir.Span{
				{Indent: true},
				{Text: "\u041E\u0443слы́ши"},
			},
			want: []ir.Span{
				{Indent: true},
				{Text: "\uA64Aслы́ши"},
			},
		},
		{
			name:  "non-digraph unchanged",
			spans: []ir.Span{{Text: "Бж҃е"}},
			want:  []ir.Span{{Text: "Бж҃е"}},
		},
		{
			name: "does not mutate input",
			spans: []ir.Span{
				{Text: "\u041E\u0443слы́ши"},
				{Text: " rest"},
			},
			want: []ir.Span{
				{Text: "\uA64Aслы́ши"},
				{Text: " rest"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original for mutation check
			origText := ""
			if len(tt.spans) > 0 {
				origText = tt.spans[0].Text
			}

			got := normalizeFirstSpanDigraphs(tt.spans)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}

			// Verify input not mutated
			if len(tt.spans) > 0 && tt.spans[0].Text != origText {
				t.Errorf("input mutated: %q → %q", origText, tt.spans[0].Text)
			}
		})
	}
}
