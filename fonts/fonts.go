// Package fonts holds the fonts of the renderers, embedded, each read by its
// name with ReadData.
//
// Fonts are embedded at compile time via go:embed from the data/ directory.
// Bukvica, which may not be redistributed, is not in data/: it is embedded
// only when a copy is there at build time (data/README.md).
package fonts

import (
	"embed"
	"fmt"
)

//go:embed data/*
var embedded embed.FS

// font is an embedded font: its name, as ReadData takes it, and its file in
// data/ when that is not NAME-Regular.otf.
type font struct {
	name     string
	filename string
}

// file is the font's file in data/.
func (f *font) file() string {
	if f.filename != "" {
		return f.filename
	}
	return f.name + "-Regular.otf"
}

// registry is every font of data/, and one that may be there (Bukvica).
var registry = []*font{
	{name: "Ponomar"},   // the text font, modern Synodal style
	{name: "Indiction"}, // drop caps, Synodal style
	{name: "Vertograd"}, // drop caps and titling, late 19th century
	{name: "Triodion"},  // a text font, a variant of the Synodal style
	{name: "Cathisma"},  // titling, Holy Synod style
	{name: "Oglavie"},   // titling, 18th to 20th century
	{name: "Pomorsky"},  // Old Ritualist calligraphic style
	{name: "Fedorovsk"}, // the style of Ivan Fedorov, 16th century
	{name: "Menaion"},   // the style of the Ustav-era manuscripts
	{name: "Monomakh"},  // academic, mixed style

	// Decorative initials from irmologion.ru. Its own notice says "free
	// for non-commercial use" and gives no license to redistribute it, so
	// it is not in data/. Placed there, Bukvica.ttf is embedded at build
	// time.
	{name: "Bukvica", filename: "Bukvica.ttf"},

	// The plain style's face, sans-serif, drawn to Calibri's widths: the
	// four variants of one family.
	{name: "Carlito", filename: "Carlito-Regular.ttf"},
	{name: "CarlitoItalic", filename: "Carlito-Italic.ttf"},
	{name: "CarlitoBold", filename: "Carlito-Bold.ttf"},
	{name: "CarlitoBoldItalic", filename: "Carlito-BoldItalic.ttf"},
}

// get returns the font of the registry by its name, or nil.
func get(name string) *font {
	for _, f := range registry {
		if f.name == name {
			return f
		}
	}
	return nil
}

// ReadData returns the raw font bytes for the given font name from the embedded FS.
func ReadData(name string) ([]byte, error) {
	f := get(name)
	if f == nil {
		return nil, fmt.Errorf("ReadData: font %q not found in registry", name)
	}
	data, err := embedded.ReadFile("data/" + f.file())
	if err != nil {
		return nil, fmt.Errorf("ReadData: %q: %w", name, err)
	}
	return data, nil
}
