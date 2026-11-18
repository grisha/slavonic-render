// Command slavonic-render typesets a Church Slavonic liturgical text, as a
// PDF or as one HTML file with its style sheet and its font inside.
//
// Usage:
//
//	slavonic-render pdf  [flags] <input.md> <output.pdf>
//	slavonic-render html [flags] <input.md> [output.html]
//
// Each subcommand lists its flags with -help. Both take -set key=value
// (repeatable), which sets a data key over the input's front matter:
// -set Lang=en renders a document in English.
//
// Without an output file, html writes the page to stdout. Its flag -head
// names a file of HTML for the end of the page's <head>, styles and scripts
// of the caller's own: examples/menu.html is a menu for the reader, the
// text's size and a dark page.
package main

import "github.com/grisha/slavonic-render/command"

func main() {
	command.Main(nil)
}
