// Package assets holds the ornaments, embedded, in two forms: the PDFs of
// data/, which the PDF renderer places, and their SVG sources in svg/, which
// the HTML renderer writes into a page. An ornament is named by its short
// name ("leafs") in both.
package assets

import (
	"embed"
	"fmt"
)

//go:embed data/*
var pdfs embed.FS

//go:embed svg/*.svg
var svgs embed.FS

// PDF returns the PDF of an ornament by its short name ("leafs"): the file
// data/ornament-NAME-red.pdf, as it is.
func PDF(name string) ([]byte, error) {
	data, err := pdfs.ReadFile("data/ornament-" + name + "-red.pdf")
	if err != nil {
		return nil, fmt.Errorf("PDF: ornament %q not found", name)
	}
	return data, nil
}

// SVG returns the SVG source of an ornament by its short name ("leafs"):
// the file svg/ornament-NAME.svg, as it is.
func SVG(name string) (string, error) {
	data, err := svgs.ReadFile("svg/ornament-" + name + ".svg")
	if err != nil {
		return "", fmt.Errorf("SVG: ornament %q not found", name)
	}
	return string(data), nil
}
