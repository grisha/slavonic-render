// Package command is the command slavonic-render, as a function a program
// can call: cmd/slavonic-render is a call of Main, and so is a program
// that gives the template stage functions of its own.
package command

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/boxesandglue/boxesandglue/backend/bag"

	"github.com/grisha/slavonic-render/pdfrender"
	"github.com/grisha/slavonic-render/pipeline"
	"github.com/grisha/slavonic-render/template"
)

// program is the name the command was run by, for its usage and its
// errors: a program that calls Main speaks under its own name.
var program = filepath.Base(os.Args[0])

// Default typographic settings of the PDF. The fonts and the drop cap's
// proportions have no flag: they are the text's, set by its front matter
// (BodyFont, DropCapFont, DropCapLines, DropCapOverhang), and these are
// what a text that sets none gets.
const (
	dftBodySize        = "14pt"
	dftMargin          = "15mm"
	dftHyphenPenalty   = 5000
	dftDropCapLines    = 2
	dftDropCapOverhang = 1.2

	dftBodyFont    = "Ponomar"
	dftDropCapFont = "Indiction"

	dftPageNumbers = "all"
	dftCatchwords  = true
)

// The files of each subcommand, as its usage shows them.
const (
	pdfFiles  = "<input.md> <output.pdf>"
	htmlFiles = "<input.md> [output.html]"
)

// newFlags makes the flag set of the subcommand name, whose files the
// usage shows as files, with the flags both subcommands take.
func newFlags(name, files string) (*flag.FlagSet, *pipeline.Config) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	cfg := &pipeline.Config{}

	fs.BoolVar(&cfg.DumpMD, "dump-md", false, "write expanded markdown to <output>.debug.md")
	fs.BoolVar(&cfg.DumpIR, "dump-ir", false, "write parsed IR as JSON to <output>.debug.json")
	cfg.Set = map[string]string{}
	fs.Var(setFlag(cfg.Set), "set", "key=value over the front matter, repeatable (Lang=en, BodyFont=Triodion)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s %s [flags] %s\n\nFlags:\n", program, name, files)
		fs.PrintDefaults()
	}

	return fs, cfg
}

// parseFiles parses args, the flags and then the files: the input and the
// output, of which at least minFiles are to be given.
func parseFiles(fs *flag.FlagSet, cfg *pipeline.Config, args []string, minFiles int) {
	fs.Parse(args)
	files := fs.Args()

	// The flag package stops at the first argument that is not a flag, so a
	// flag after the file names would be ignored silently: refuse it.
	if len(files) > 2 {
		fmt.Fprintf(os.Stderr, "%s: %q after the output file: flags go before the input and output files\n\n",
			program, strings.Join(files[2:], " "))
	}
	if len(files) < minFiles || len(files) > 2 {
		fs.Usage()
		os.Exit(1)
	}

	cfg.InputPath = files[0]
	if len(files) == 2 {
		cfg.OutputPath = files[1]
	}
}

func pdfFlags(args []string) (cfg *pipeline.Config, pdf pdfrender.DocumentConfig) {
	fs, cfg := newFlags("pdf", pdfFiles)
	pdf.BodyFontName = dftBodyFont
	pdf.DropCapFontName = dftDropCapFont
	pdf.DropCap.Lines = dftDropCapLines
	pdf.DropCap.Overhang = dftDropCapOverhang

	// The PDF's settings (the lengths parsed from strings after the flags)
	var bodySize, leading, margin, dropCapMargin, pageNumbers string
	fs.StringVar(&bodySize, "body-size", dftBodySize, "body text font size")
	fs.StringVar(&leading, "leading", "", "baseline-to-baseline spacing (default: in proportion to -body-size)")
	fs.StringVar(&margin, "margin", dftMargin, "page margin (all sides)")
	fs.IntVar(&pdf.HyphenPenalty, "hyphen-penalty", dftHyphenPenalty, "higher = fewer hyphens")
	fs.StringVar(&dropCapMargin, "drop-cap-margin", "", "gap between drop cap and text (default: in proportion to -body-size)")
	fs.StringVar(&pageNumbers, "page-numbers", dftPageNumbers, "which pages are numbered: none/all/leaf (odd pages, both sides of a leaf sharing the number); the numerals are the document's (-set Numerals=arabic)")
	fs.BoolVar(&pdf.Catchwords, "catchwords", dftCatchwords, "show catchwords at page bottom")

	parseFiles(fs, cfg, args, 2)

	// Parse dimension strings into ScaledPoints
	for _, d := range []struct {
		name string
		val  string
		dest *bag.ScaledPoint
	}{
		{"body-size", bodySize, &pdf.BodySize},
		{"leading", leading, &pdf.Leading},
		{"margin", margin, &pdf.Margin},
		{"drop-cap-margin", dropCapMargin, &pdf.DropCap.Margin},
	} {
		if d.val == "" {
			continue // pdfrender sets it in proportion to the body size
		}
		sp, spErr := bag.SP(d.val)
		if spErr != nil {
			log.Fatalf("Invalid --%s %q: %v", d.name, d.val, spErr)
		}
		*d.dest = sp
	}

	// Parse page numbering
	var err error
	pdf.PageNumbering, err = parsePageNumbering(pageNumbers)
	if err != nil {
		log.Fatalf("%v", err)
	}

	return cfg, pdf
}

func htmlFlags(args []string) (cfg *pipeline.Config, head string) {
	fs, cfg := newFlags("html", htmlFiles)

	// The page's settings (the file read after the flags)
	var headPath string
	fs.StringVar(&headPath, "head", "", "a file of HTML for the end of the page's <head>: styles and scripts of your own (examples/menu.html)")

	// Without an output file the page goes to stdout
	parseFiles(fs, cfg, args, 1)

	// Read the -head file
	if headPath != "" {
		data, err := os.ReadFile(headPath)
		if err != nil {
			log.Fatalf("Reading the -head file: %v", err)
		}
		head = string(data)
	}

	return cfg, head
}

// setFlag collects the -set key=value flags into a map.
type setFlag map[string]string

func (s setFlag) String() string { return fmt.Sprint(map[string]string(s)) }

func (s setFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("setFlag.Set: %q is not key=value", v)
	}
	s[k] = val
	return nil
}

// parsePageNumbering maps a flag string to a pdfrender.PageNumbering constant.
func parsePageNumbering(s string) (pdfrender.PageNumbering, error) {
	switch s {
	case "none":
		return pdfrender.PageNumbersNone, nil
	case "all":
		return pdfrender.PageNumbersAll, nil
	case "leaf":
		return pdfrender.PageNumbersLeaf, nil
	default:
		return 0, fmt.Errorf("parsePageNumbering: unknown value %q (valid: none, all, leaf)", s)
	}
}

// usage prints the subcommands.
func usage() {
	fmt.Fprintf(os.Stderr, "Usage:\n  %[1]s pdf  [flags] %[2]s\n  %[1]s html [flags] %[3]s\n\n"+
		"The flags of each: %[1]s pdf -help, %[1]s html -help\n", program, pdfFiles, htmlFiles)
}

// templateOptions gives cfg the caller's options of the template stage,
// made for this run by options.
func templateOptions(cfg *pipeline.Config, options func(cfg *pipeline.Config) ([]template.ProcessOption, error)) {
	if options == nil {
		return
	}
	opts, err := options(cfg)
	if err != nil {
		log.Fatalf("%s: %v", program, err)
	}
	cfg.Template = opts
}

// Main is the command: it reads os.Args, renders, and does not return when
// it fails (it exits, as a command does). The usage and the errors name
// the program by the name it was run by.
//
// options, when not nil, gives the template stage's options for the run:
// the functions a caller adds (template.WithFunc). It is called once the
// flags are read, with the run's configuration (the input file, the -set
// keys), so that a function can be made for the document at hand, knowing
// its language or where its file lies.
func Main(options func(cfg *pipeline.Config) ([]template.ProcessOption, error)) {
	subcommand := ""
	if len(os.Args) > 1 {
		subcommand = os.Args[1]
	}

	var err error
	switch subcommand {
	case "pdf":
		cfg, pdf := pdfFlags(os.Args[2:])
		templateOptions(cfg, options)
		err = pipeline.RunPDF(cfg, pdf)
	case "html":
		cfg, head := htmlFlags(os.Args[2:])
		templateOptions(cfg, options)
		err = pipeline.RunHTML(cfg, head)
	case "-h", "-help", "--help":
		usage()
	default:
		if subcommand != "" {
			fmt.Fprintf(os.Stderr, "%s: %q is not a subcommand\n\n", program, subcommand)
		}
		usage()
		os.Exit(1)
	}
	if err != nil {
		log.Fatalf("%s: %v", program, err)
	}
}
