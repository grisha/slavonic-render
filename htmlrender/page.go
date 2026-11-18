package htmlrender

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"html"
	"io"

	"github.com/grisha/slavonic-render/fonts"
	"github.com/grisha/slavonic-render/ir"
)

// defaultBodyFont is the face the style sheet names for the text (--body
// in data/text.css; a test holds the two together).
const defaultBodyFont = "Ponomar"

// The page around a document, in four pieces: up to the font, from the font
// to the style sheet, from the style sheet to the caller's part of the
// head, and around the document. pageBodyFont is a fifth, written after
// the style sheet when the text's face is not the style sheet's own.
const (
	pageHead = `<!DOCTYPE html>
<html lang="%s">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
@font-face {
	font-family: "%s";
	src: url("data:font/otf;base64,`
	pageFontEnd = `") format("opentype");
}
body {
	margin: 1em 0;
}
/* On a screen the page follows the device, dark when it is set to dark
   (the browser's own dark background and light text, and the style
   sheet's lighter red), unless data-theme on the root element says
   light or dark: that is for a script to set, as a reader's choice.
   Printed, the page is always light; were a script to set color-scheme
   itself, a dark choice would print. */
@media screen {
	:root {
		color-scheme: light dark;
	}
	:root[data-theme="light"] {
		color-scheme: light;
	}
	:root[data-theme="dark"] {
		color-scheme: dark;
	}
}
`
	pageBodyFont = `.slavonic {
	--body: "%s", serif;
}
`
	pageStyleEnd = `</style>
`
	pageBody = `</head>
<body>
`
	pageEnd = `</body>
</html>
`
)

// RenderPage writes doc to w as a whole page that needs nothing else: the
// style sheet and the font are inside it. The file opens from a disk, where
// there is no server to ask for a font, and can be sent as it is. On a
// screen it is dark when the device is set to dark. lang is the language
// of the document ("cu", "en"), which the page declares. frontMatter is
// Render's: the keys the document's expressions may name.
//
// bodyFont is the font of the text, by the name fonts.ReadData takes
// ("Triodion"): it is the font inside the page, and the style sheet's
// --body is set to it. Empty, the font is the style sheet's own, Ponomar.
//
// head is the caller's part of the page, HTML written as it is at the end
// of the page's <head>, after the style sheet: styles and scripts of the
// caller's own. A script there can set the text's size (the custom
// property --size on the root element, a factor) and the theme (the
// attribute data-theme on it, "light" or "dark"); examples/menu.html is
// a menu that does.
func RenderPage(w io.Writer, doc *ir.Document, frontMatter map[string]string, title, lang, bodyFont, head string) error {
	face := bodyFont
	if face == "" {
		face = defaultBodyFont
	}
	font, err := fonts.ReadData(face)
	if err != nil {
		return fmt.Errorf("RenderPage1: %w", err)
	}

	// face is written into the style as it is: it is a name of the fonts'
	// registry, ReadData having found it.
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, pageHead, html.EscapeString(lang), html.EscapeString(title), face)
	// The font goes into the style sheet as a data: URI, in base64. The
	// encoder is a writer over the page's writer; Close writes what is left
	// of the last three bytes, it does not close the page.
	enc := base64.NewEncoder(base64.StdEncoding, bw)
	enc.Write(font)
	enc.Close()
	bw.WriteString(pageFontEnd)
	bw.WriteString(CSS)
	if bodyFont != "" {
		fmt.Fprintf(bw, pageBodyFont, bodyFont)
	}
	bw.WriteString(pageStyleEnd)
	bw.WriteString(head)
	bw.WriteString(pageBody)
	if err := Render(bw, doc, frontMatter); err != nil {
		return fmt.Errorf("RenderPage2: %w", err)
	}
	bw.WriteString(pageEnd)
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("RenderPage3: %w", err)
	}
	return nil
}
