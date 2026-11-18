package pdfrender

import (
	"fmt"

	"github.com/boxesandglue/boxesandglue/frontend"

	"github.com/grisha/slavonic-render/fonts"
)

// DocumentConfig is what NewDocumentFromConfig needs: the file, the title,
// the fonts, the front matter, the typesetting and the page.
type DocumentConfig struct {
	OutputPath string // the PDF file, created by NewDocumentFromConfig
	Title      string // the PDF's title, in its metadata

	// The fonts, by the names fonts.ReadData takes ("Ponomar").
	BodyFontName    string
	DropCapFontName string

	// FrontMatter holds the parsed front matter key-value pairs, made
	// available as variables in ${...} body-text expressions.
	FrontMatter map[string]string

	Config     // embeds render config (BodySize, Leading, DropCap, HyphenPenalty, etc.)
	PageConfig // embeds page config (Margin, PageNumbering, Numeral, Catchwords)
}

// NewDocumentFromConfig creates a fully-initialized DocumentRenderer.
// It creates the boxesandglue document, loads fonts, sets up hyphenation,
// and wires together the renderer and the page builder.
func NewDocumentFromConfig(cfg DocumentConfig) (*DocumentRenderer, error) {
	// Create the boxesandglue document.
	doc, err := frontend.New(cfg.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("NewDocumentFromConfig1: %w", err)
	}
	doc.Doc.Title = cfg.Title

	// Load font families.
	fnts, err := loadFonts(doc, cfg.BodyFontName, cfg.DropCapFontName)
	if err != nil {
		return nil, fmt.Errorf("NewDocumentFromConfig2: %w", err)
	}
	cfg.Config.fontFamilies = fnts

	// Load hyphenation.
	cuLang, err := loadChurchSlavonicHyphenation()
	if err != nil {
		return nil, fmt.Errorf("NewDocumentFromConfig3: %w", err)
	}
	cfg.Config.language = cuLang

	r := newRenderer(doc, cfg.Config)
	pb := newPageBuilder(r, doc, "", cfg.PageConfig)

	return newDocumentRenderer(r, pb, cfg.FrontMatter), nil
}

// loadFonts loads the body, drop cap, and plain font families into the document.
// The plain family is not a choice: it needs four variants, and Carlito is the
// one embedded font that has them.
func loadFonts(doc *frontend.Document, bodyFont, dropCapFont string) (*fontFamilies, error) {
	add := func(ff *frontend.FontFamily, name string, w frontend.FontWeight, s frontend.FontStyle) error {
		data, err := fonts.ReadData(name)
		if err != nil {
			return fmt.Errorf("loadFonts: %w", err)
		}
		return ff.AddMember(&frontend.FontSource{Data: data}, w, s)
	}

	text := doc.NewFontFamily("text")
	if err := add(text, bodyFont, frontend.FontWeight400, frontend.FontStyleNormal); err != nil {
		return nil, err
	}

	drop := doc.NewFontFamily("dropcap")
	if err := add(drop, dropCapFont, frontend.FontWeight400, frontend.FontStyleNormal); err != nil {
		return nil, err
	}

	plain := doc.NewFontFamily("plain")
	if err := add(plain, "Carlito", frontend.FontWeight400, frontend.FontStyleNormal); err != nil {
		return nil, err
	}
	if err := add(plain, "CarlitoItalic", frontend.FontWeight400, frontend.FontStyleItalic); err != nil {
		return nil, err
	}
	if err := add(plain, "CarlitoBold", frontend.FontWeight700, frontend.FontStyleNormal); err != nil {
		return nil, err
	}
	if err := add(plain, "CarlitoBoldItalic", frontend.FontWeight700, frontend.FontStyleItalic); err != nil {
		return nil, err
	}

	return &fontFamilies{bodyFont: text, dropCapFont: drop, plainFont: plain}, nil
}
