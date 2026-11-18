// Package template provides Stage 1 of the template-to-PDF pipeline:
// expanding Go text/template actions and reading the front matter.
//
// Template functions available in .md source files:
//
//   - {{ include "path" }}                          → include another template file (relative path)
//   - {{ include "path" | numberVerses }}           → join verses into one paragraph with &{margin} markers, the document's numerals, «сⷯ» before the first in cu
//   - {{ ... | verseRange 5 10 }}                   → extract verses 5–10 (by counting &{margin} markers from numberVerses)
//   - {{ $t := include "path" | paragraphs }}       → split into a list of paragraphs for range/index/slice/len
//   - {{ ... | firstVerseAsHeading }}               → first verse becomes centered rubric heading
//   - {{ ... | firstVerseAsHeading 2 "dropcap" }}   → first 2 verses as heading, body gets drop cap
//   - {{ set "key" "value" }}                       → set a data key for everything expanded after it
//
// A caller adds functions of its own with WithFunc: this package knows none
// of them.
//
// A number in a text is marked &{num к҃ѕ} or &{num 26}, either way; the
// last step of ProcessTemplate writes it in the document's numerals, the key
// Numerals: cyrillic or arabic, cyrillic when not set for a document in cu
// and arabic in any other language.
//
// A key that is not set is an error: ProcessTemplate fails on a «<no value>»
// or a «%!s(<nil>)» left in the expanded text.
//
// Front matter (lines of key: value between --- delimiters at file start):
package template

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	gotmpl "text/template"

	"github.com/grisha/slavonic-render/cu"
)

// ProcessOption configures template processing.
type ProcessOption func(*templateProcessor)

// WithData sets data keys over the front matter (the command's flag -set),
// e.g. Lang=en.
func WithData(values map[string]string) ProcessOption {
	return func(tp *templateProcessor) {
		if fm, ok := tp.data.(map[string]string); ok {
			for k, v := range values {
				fm[k] = v
			}
		}
	}
}

// WithFunc registers an additional template function by name.
func WithFunc(name string, fn any) ProcessOption {
	return func(tp *templateProcessor) {
		tp.funcMap[name] = fn
	}
}

// ProcessTemplate processes the template file name of the tree fsys and
// returns the resulting flat Markdown and parsed front matter key-value
// pairs. The front matter is used as the data context for template
// execution; the one returned always says Lang and Numerals, the defaults
// when the template sets neither. Every file is read from fsys: an include path is relative to
// the directory of the file containing the {{ include }} action and may
// climb with "..", though not above the root of fsys.
func ProcessTemplate(fsys fs.FS, name string, opts ...ProcessOption) (markdown string, frontMatter map[string]string, err error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", nil, fmt.Errorf("ProcessTemplate: reading %s: %w", name, err)
	}

	body, fm := stripFrontMatter(string(raw))
	tp := newTemplateProcessor(fsys, fm)
	for _, opt := range opts {
		opt(tp)
	}
	fm["Lang"] = tp.lang()
	fm["Numerals"] = tp.numerals()
	write, err := tp.writer()
	if err != nil {
		return "", nil, err
	}
	result, err := tp.processBody(path.Base(name), path.Dir(name), body)
	if err != nil {
		return "", nil, err
	}
	result, err = expandNum(write, result)
	if err != nil {
		return "", nil, fmt.Errorf("ProcessTemplate1: %w", err)
	}
	if err := findHole(result); err != nil {
		return "", nil, fmt.Errorf("ProcessTemplate2: %w", err)
	}
	return result, fm, nil
}

// templateProcessor holds shared state for template execution.
// Both the top-level ProcessTemplate and nested {{ include }} calls
// use the same process/processBody methods, avoiding code duplication.
type templateProcessor struct {
	fsys    fs.FS           // the tree every file is read from
	funcMap gotmpl.FuncMap  // all template functions except "include"
	stack   map[string]bool // circular-include detection (shared across nesting levels)
	data    any             // template execution data (top-level front matter)
}

// newTemplateProcessor creates a processor with the standard template functions.
// fsys is the tree it reads from; data is passed to every template.Execute
// call (both top-level and includes).
func newTemplateProcessor(fsys fs.FS, data any) *templateProcessor {
	tp := &templateProcessor{
		fsys: fsys,
		funcMap: gotmpl.FuncMap{
			"verseRange":          verseRange,
			"firstVerseAsHeading": firstVerseAsHeading,
			"paragraphs":          paragraphs,
		},
		stack: make(map[string]bool),
		data:  data,
	}
	tp.funcMap["set"] = tp.set
	tp.funcMap["numberVerses"] = func(text string) (string, error) {
		write, err := tp.writer()
		if err != nil {
			return "", err
		}
		// «сⷯ» before the first verse's number in a Church Slavonic document
		// only: by its language, not its numerals (one in arabic still
		// writes it).
		prefix := ""
		if tp.lang() == "cu" {
			prefix = stixPrefix
		}
		return numberVerses(write, prefix, text), nil
	}
	return tp
}

// funcMapFor clones the base function map and adds "include", which
// resolves a path from dir, the directory of the file in the tree.
func (tp *templateProcessor) funcMapFor(dir string) gotmpl.FuncMap {
	fm := make(gotmpl.FuncMap, len(tp.funcMap)+1)
	for k, v := range tp.funcMap {
		fm[k] = v
	}
	fm["include"] = tp.includeFrom(dir)
	return fm
}

// lang is the document's language, the data key Lang (front matter or the
// command's flag -set Lang=en); cu when not set.
func (tp *templateProcessor) lang() string {
	if fm, ok := tp.data.(map[string]string); ok && fm["Lang"] != "" {
		return fm["Lang"]
	}
	return "cu"
}

// numerals is the document's numerals, the data key Numerals (front matter
// or -set Numerals=arabic): cyrillic or arabic; when not set, cyrillic for
// a document in cu, arabic in any other language.
func (tp *templateProcessor) numerals() string {
	if fm, ok := tp.data.(map[string]string); ok && fm["Numerals"] != "" {
		return fm["Numerals"]
	}
	if tp.lang() == "cu" {
		return "cyrillic"
	}
	return "arabic"
}

// writer is the writer of the document's numerals; a value of Numerals
// other than cyrillic or arabic is an error naming the key.
func (tp *templateProcessor) writer() (func(int) string, error) {
	write, err := cu.NumeralWriter(tp.numerals())
	if err != nil {
		return nil, fmt.Errorf("templateProcessor.writer: the key Numerals: %w", err)
	}
	return write, nil
}

// processBody is the core template pipeline: parse body as a Go template
// and execute it.
func (tp *templateProcessor) processBody(name, dir, body string) (string, error) {
	tmpl, err := gotmpl.New(name).Funcs(tp.funcMapFor(dir)).Parse(body)
	if err != nil {
		return "", fmt.Errorf("templateProcessor.processBody: parsing %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tp.data); err != nil {
		return "", fmt.Errorf("templateProcessor.processBody: executing %s: %w", name, err)
	}

	return buf.String(), nil
}

// process reads the file name of the tree, strips front matter, and
// processes the template. Used by includeFrom for nested includes.
func (tp *templateProcessor) process(name string) (string, map[string]string, error) {
	raw, err := fs.ReadFile(tp.fsys, name)
	if err != nil {
		return "", nil, fmt.Errorf("templateProcessor.process: reading %s: %w", name, err)
	}

	body, fm := stripFrontMatter(string(raw))
	result, err := tp.processBody(path.Base(name), path.Dir(name), body)
	if err != nil {
		return "", nil, err
	}
	return result, fm, nil
}

// includeFrom returns a template function that includes another template
// file. A path is relative to baseDir, a directory of the tree, and may
// climb with ".."; one that climbs above the tree's root is refused.
// Circular includes are detected.
func (tp *templateProcessor) includeFrom(baseDir string) func(string) (string, error) {
	return func(name string) (string, error) {
		fullPath := path.Join(baseDir, name)
		// A joined path is clean, so it is invalid only when it begins with "..".
		if !fs.ValidPath(fullPath) {
			return "", fmt.Errorf("templateProcessor.includeFrom: include %q: outside the tree", name)
		}

		if tp.stack[fullPath] {
			return "", fmt.Errorf("templateProcessor.includeFrom: include %q: circular include detected", name)
		}
		tp.stack[fullPath] = true
		defer delete(tp.stack, fullPath)

		result, _, err := tp.process(fullPath)
		if err != nil {
			return "", fmt.Errorf("templateProcessor.includeFrom: include %q: %w", name, err)
		}
		return result, nil
	}
}
