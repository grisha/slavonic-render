package cu

import (
	"fmt"
	"testing"
)

func TestToCyrillicNumeral(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "а҃"},
		{2, "в҃"},
		{3, "г҃"},
		{9, "ѳ҃"},
		{10, "і҃"},
		{11, "а҃і"}, // Special case: units before tens for 11-19
		{12, "в҃і"},
		{15, "є҃і"},
		{19, "ѳ҃і"},
		{20, "к҃"},
		{21, "к҃а"},
		{90, "ч҃"},
		{99, "ч҃ѳ"},
		{100, "р҃"},
		{123, "рк҃г"}, // 100 + 20 + 3
		{500, "ф҃"},
		{900, "ц҃"},
		{1000, "҂а҃"},
		{1564, "҂афѯ҃д"}, // 1000 + 500 + 60 + 4
		{7071, "҂зѻ҃а"},  // 7000 + 70 + 1
		{6091, "҂ѕч҃а"},  // 6000 + 90 + 1
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.n), func(t *testing.T) {
			got := ToCyrillicNumeral(tt.n)
			if got != tt.want {
				t.Errorf("ToCyrillicNumeral(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestParseNumeralRoundTrip(t *testing.T) {
	for n := 1; n < 10000; n++ {
		got, err := ParseNumeral(ToCyrillicNumeral(n))
		if err != nil || got != n {
			t.Fatalf("ParseNumeral(%q) = %d, %v; want %d", ToCyrillicNumeral(n), got, err, n)
		}
	}
}

func TestParseNumeral(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"233", 233},     // Arabic
		{"слг", 233},     // no titlo
		{"с҃лг", 233},    // titlo misplaced
		{"сл҃г", 233},    // canonical
		{"і҃а", 11},      // tens first
		{"ѻ҃", 70},       // 70
		{"о҃а", 71},      // о for ѻ
		{"ѿ҃", 800},      // variant of ѡ
		{"҂а҃", 1000},    // thousands
		{"҂афѯ҃д", 1564}, // thousands, then the rest
	}
	for _, tt := range tests {
		got, err := ParseNumeral(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseNumeral(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "слб", "мч҃нкъ", "҂", "12a"} {
		if n, err := ParseNumeral(bad); err == nil {
			t.Errorf("ParseNumeral(%q) = %d, want an error", bad, n)
		}
	}
}

func TestNumeralWriter(t *testing.T) {
	for style, want := range map[string]string{"cyrillic": "сл҃г", "arabic": "233"} {
		w, err := NumeralWriter(style)
		if err != nil || w(233) != want {
			t.Errorf("NumeralWriter(%q)(233) = %q, %v; want %q", style, w(233), err, want)
		}
	}
	if _, err := NumeralWriter("roman"); err == nil {
		t.Error("NumeralWriter(\"roman\"): want an error")
	}
}
