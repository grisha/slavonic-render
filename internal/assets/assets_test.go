package assets

import (
	"strings"
	"testing"
)

// Every ornament of data/ is a PDF, read by its short name, and has its
// SVG, so the HTML renderer can show it too; an unknown ornament is an
// error naming it, in both forms.
func TestOrnaments(t *testing.T) {
	files, err := pdfs.ReadDir("data")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("no ornament in data/")
	}
	for _, pdf := range files {
		name, ok := strings.CutPrefix(pdf.Name(), "ornament-")
		if ok {
			name, ok = strings.CutSuffix(name, "-red.pdf")
		}
		if !ok {
			t.Errorf("data/%s is not named ornament-NAME-red.pdf", pdf.Name())
			continue
		}
		data, err := PDF(name)
		if err != nil || !strings.HasPrefix(string(data), "%PDF") {
			t.Errorf("PDF(%q): %v, not a PDF", name, err)
		}
		svg, err := SVG(name)
		if err != nil {
			t.Errorf("%s has a PDF and no SVG: %v", name, err)
			continue
		}
		if !strings.Contains(svg, "<svg") {
			t.Errorf("svg/ornament-%s.svg has no <svg", name)
		}
	}

	if _, err := PDF("nosuch"); err == nil || !strings.Contains(err.Error(), `"nosuch"`) {
		t.Errorf("PDF of an unknown ornament: got %v, want an error naming it", err)
	}
	if _, err := SVG("nosuch"); err == nil || !strings.Contains(err.Error(), `"nosuch"`) {
		t.Errorf("SVG of an unknown ornament: got %v, want an error naming it", err)
	}
}
