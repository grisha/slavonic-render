package pdfrender

import (
	"path/filepath"
	"testing"

	"github.com/boxesandglue/boxesandglue/backend/bag"
	"github.com/boxesandglue/boxesandglue/frontend"
)

// The leading and the drop cap's gap follow the body size unless they are
// set.
func TestDefaultLeading(t *testing.T) {
	doc, err := frontend.New(filepath.Join(t.TempDir(), "leading.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	fnts, err := loadFonts(doc, "Ponomar", "Indiction")
	if err != nil {
		t.Fatal(err)
	}
	rendererAt := func(cfg Config) *renderer {
		cfg.fontFamilies = fnts
		return newRenderer(doc, cfg)
	}

	r := rendererAt(Config{BodySize: bag.MustSP("14pt")})
	if r.Leading != bag.MustSP("20pt") {
		t.Errorf("leading at 14pt = %v, want 20pt", r.Leading)
	}
	if r.DropCap.Margin != bag.MustSP("3pt") {
		t.Errorf("drop cap margin at 14pt = %v, want 3pt", r.DropCap.Margin)
	}
	// The vertical spacing in fractions of the leading: a fraction that is
	// a whole number of points is exactly that.
	for _, c := range []struct {
		f    float64
		want string
	}{{0.6, "12pt"}, {0.5, "10pt"}, {0.3, "6pt"}, {0.25, "5pt"}, {0.2, "4pt"}, {0.15, "3pt"}, {0.1, "2pt"}} {
		if got := r.lead(c.f); got != bag.MustSP(c.want) {
			t.Errorf("lead(%v) at 14pt = %v, want %s", c.f, got, c.want)
		}
	}

	r = rendererAt(Config{BodySize: bag.MustSP("21pt")})
	if r.Leading != bag.MustSP("30pt") {
		t.Errorf("leading at 21pt = %v, want 30pt", r.Leading)
	}

	r = rendererAt(Config{BodySize: bag.MustSP("14pt"), Leading: bag.MustSP("18pt"),
		DropCap: DropCapConfig{Margin: bag.MustSP("5pt")}})
	if r.Leading != bag.MustSP("18pt") || r.DropCap.Margin != bag.MustSP("5pt") {
		t.Errorf("explicit leading, margin = %v, %v, want 18pt, 5pt", r.Leading, r.DropCap.Margin)
	}
}
