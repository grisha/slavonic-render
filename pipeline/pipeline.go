// Package pipeline runs the stages together: the template → markdown → the
// parsed document, which both formats share, then the PDF (RunPDF) or the
// page of HTML (RunHTML).
package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/grisha/slavonic-render/cu"
	"github.com/grisha/slavonic-render/htmlrender"
	"github.com/grisha/slavonic-render/ir"
	"github.com/grisha/slavonic-render/markup"
	"github.com/grisha/slavonic-render/pdfrender"
	"github.com/grisha/slavonic-render/template"
)

// Config holds what a run of either format needs: the files, the dumps,
// and the data and options of the template stage.
type Config struct {
	InputPath  string
	OutputPath string
	DumpMD     bool
	DumpIR     bool
	Set        map[string]string // data keys over the front matter (-set key=value)

	// Template holds the caller's options of the template stage: the
	// functions it adds (template.WithFunc), passed on to ExpandFile.
	Template []template.ProcessOption
}

// ExpandFile runs the template stage, the one every output format shares,
// on the template at inputPath: it gives the flat Markdown and the front
// matter, with the keys of set over the front matter. The tree is the disk
// from its root, so the template may include any file, above its own
// directory too ("../"). opts are the caller's options of the template
// stage, the functions it adds among them (template.WithFunc). It prints
// nothing, so a caller may write its own output to stdout. A template in
// a tree of another kind (an embed.FS) is template.ProcessTemplate's.
func ExpandFile(inputPath string, set map[string]string, opts ...template.ProcessOption) (markdown string, frontMatter map[string]string, err error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return "", nil, fmt.Errorf("ExpandFile1: %w", err)
	}
	opts = append([]template.ProcessOption{template.WithData(set)}, opts...)
	markdown, frontMatter, err = template.ProcessTemplate(os.DirFS("/"), strings.TrimPrefix(abs, "/"), opts...)
	if err != nil {
		return "", nil, fmt.Errorf("ExpandFile2: %w", err)
	}
	return markdown, frontMatter, nil
}

// Title is the document's title, which the PDF carries in its metadata and
// the HTML page in its <title>: the front matter's DocumentTitle, and
// without it the text of the document's first # heading. A document with
// neither has no title, and a viewer names it by its file.
func Title(frontMatter map[string]string, doc *ir.Document) string {
	if title := frontMatter["DocumentTitle"]; title != "" {
		return title
	}
	for _, b := range doc.Blocks {
		if b.Kind == ir.BlockHeading1 {
			return b.Text
		}
	}
	return ""
}

// parseFile runs the stages every output format shares on the template at
// cfg.InputPath: the template stage, then the markup. It gives the parsed
// document and the front matter, with the keys of cfg.Set over it.
//
// It makes the directory of cfg.OutputPath, for the caller's file and for
// the dumps cfg asks for, which are written beside that file and named
// after it: without an output file (RunHTML's page going to stdout) a
// dump is an error. A dump that cannot be written is a warning. The
// stages are reported on stderr: stdout is the caller's, and may be where
// the rendered document goes.
func parseFile(cfg *Config) (doc *ir.Document, frontMatter map[string]string, err error) {
	if cfg.OutputPath == "" {
		if cfg.DumpMD || cfg.DumpIR {
			return nil, nil, fmt.Errorf("parseFile: a dump is named after the output file, and there is none")
		}
	} else if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("parseFile: creating output directory: %w", err)
	}

	// Stage 1: Template expansion → flat markdown + front matter.
	fmt.Fprintf(os.Stderr, "Stage 1: Processing template %s...\n", filepath.Base(cfg.InputPath))
	flatMarkdown, frontMatter, err := ExpandFile(cfg.InputPath, cfg.Set, cfg.Template...)
	if err != nil {
		return nil, nil, fmt.Errorf("parseFile1: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Front matter: %q\n", frontMatter)
	fmt.Fprintf(os.Stderr, "  Output: %d bytes of flat Markdown\n", len(flatMarkdown))

	if cfg.DumpMD {
		debugPath := cfg.OutputPath + ".debug.md"
		if err := os.WriteFile(debugPath, []byte(flatMarkdown), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not write debug markdown: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Debug markdown written to: %s\n", debugPath)
		}
	}

	// Stage 2: Parse markup → IR.
	fmt.Fprintln(os.Stderr, "Stage 2: Parsing Markdown...")
	doc, err = markup.Parse(flatMarkdown)
	if err != nil {
		return nil, nil, fmt.Errorf("parseFile2: %w", err)
	}

	if cfg.DumpIR {
		debugPath := cfg.OutputPath + ".debug.json"
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not marshal IR: %v\n", err)
		} else if err := os.WriteFile(debugPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not write debug IR: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Debug IR written to: %s\n", debugPath)
		}
	}

	return doc, frontMatter, nil
}

// RunPDF executes the full template → markdown → PDF pipeline, reporting
// its progress on stderr: the template at cfg.InputPath becomes the PDF
// at cfg.OutputPath. pdf is the PDF's configuration. Four of its fields
// are set here: OutputPath, from cfg; Title, FrontMatter and Numeral,
// from the front matter. The front matter's keys BodyFont, DropCapFont,
// DropCapLines and DropCapOverhang, when set, replace the fonts and the
// drop cap's proportions given in pdf.
func RunPDF(cfg *Config, pdf pdfrender.DocumentConfig) error {
	// Stages 1 and 2: the template → the parsed document.
	doc, frontMatter, err := parseFile(cfg)
	if err != nil {
		return fmt.Errorf("RunPDF1: %w", err)
	}

	// Stage 3: Render IR → PDF.
	fmt.Fprintln(os.Stderr, "Stage 3: Rendering PDF...")
	pdf.OutputPath = cfg.OutputPath
	pdf.Title = Title(frontMatter, doc)
	pdf.FrontMatter = frontMatter
	if err := applyFrontMatter(frontMatter, &pdf); err != nil {
		return fmt.Errorf("RunPDF: front matter: %w", err)
	}

	dr, err := pdfrender.NewDocumentFromConfig(pdf)
	if err != nil {
		return fmt.Errorf("RunPDF2: %w", err)
	}
	if err := dr.Render(doc); err != nil {
		return fmt.Errorf("RunPDF3: %w", err)
	}

	if dr.HasDeferred() {
		fmt.Fprintln(os.Stderr, "  Rendering deferred content (resolving forward references)...")
		if err := dr.RenderDeferred(); err != nil {
			return fmt.Errorf("RunPDF4: %w", err)
		}
	}

	if err := dr.Finish(); err != nil {
		return fmt.Errorf("RunPDF5: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nOutput saved to: %s\n", cfg.OutputPath)
	return nil
}

// RunHTML executes the full template → markdown → HTML pipeline, reporting
// its progress on stderr: the template at cfg.InputPath becomes one page of
// HTML (htmlrender.RenderPage) in cfg.OutputPath, or on stdout when
// cfg.OutputPath is empty. head is the caller's HTML for the end of the
// page's <head>. The front matter's key BodyFont, when set, is the font of
// the text; DropCapFont, DropCapLines and DropCapOverhang are the PDF's.
func RunHTML(cfg *Config, head string) error {
	// Stages 1 and 2: the template → the parsed document.
	doc, frontMatter, err := parseFile(cfg)
	if err != nil {
		return fmt.Errorf("RunHTML1: %w", err)
	}

	// Stage 3: Render IR → HTML. The page is made whole before any of it
	// is written: a render that fails (an expression naming no key) leaves
	// nothing behind that looks like a page, in a file or on stdout, and a
	// file of an earlier run stays as it was.
	fmt.Fprintln(os.Stderr, "Stage 3: Rendering HTML...")
	var page bytes.Buffer
	if err := htmlrender.RenderPage(&page, doc, frontMatter, Title(frontMatter, doc), frontMatter["Lang"], frontMatter["BodyFont"], head); err != nil {
		return fmt.Errorf("RunHTML2: %w", err)
	}
	if cfg.OutputPath == "" {
		if _, err := os.Stdout.Write(page.Bytes()); err != nil {
			return fmt.Errorf("RunHTML3: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(cfg.OutputPath, page.Bytes(), 0o666); err != nil {
		return fmt.Errorf("RunHTML4: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nOutput saved to: %s\n", cfg.OutputPath)
	return nil
}

// applyFrontMatter overrides the PDF's configuration with values from front matter;
// PageNumerals gives the writer of the page numbers, the footer's and the
// contents' (cyrillic or arabic), the document's Numerals when not set (set
// by the template stage, from Lang when the document has none). A value that
// cannot be read is an error naming its key.
func applyFrontMatter(fm map[string]string, pdf *pdfrender.DocumentConfig) error {
	style := fm["PageNumerals"]
	if style == "" {
		style = fm["Numerals"]
	}
	write, err := cu.NumeralWriter(style)
	if err != nil {
		return fmt.Errorf("applyFrontMatter: the key PageNumerals: %w", err)
	}
	pdf.Numeral = write
	if v, ok := fm["BodyFont"]; ok {
		pdf.BodyFontName = v
	}
	if v, ok := fm["DropCapFont"]; ok {
		pdf.DropCapFontName = v
	}
	if v, ok := fm["DropCapLines"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("applyFrontMatter: the key DropCapLines: %w", err)
		}
		pdf.DropCap.Lines = n
	}
	if v, ok := fm["DropCapOverhang"]; ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("applyFrontMatter: the key DropCapOverhang: %w", err)
		}
		pdf.DropCap.Overhang = f
	}
	return nil
}
