package cu

import (
	_ "embed"
	"io"
	"strings"
)

// Church Slavonic hyphenation patterns in TeX format (Franklin Liang's
// algorithm). Each pattern is a word fragment annotated with digits that encode
// break priorities at every inter-character position: odd digits allow a
// hyphen, even digits (including implicit zeros) forbid one. The hyphenation
// engine overlays all matching patterns and takes the maximum digit at each
// position to decide where breaks are permitted.
//
// Source: derived from https://github.com/slavonic/cu-tex/blob/master/hyphenation/hyph-cu.tex,
// distributed via the hyph-utf8 project (https://github.com/hyphenation/tex-hyphen).
//
// License: MIT
// Authors: Aleksandr Andreev and Mike Kroutikov
//
//go:embed hyph-cu.txt
var hyphenPatterns string

const (
	// HyphenLeftMin is the minimum number of characters before the first
	// hyphenation point (polyglossia \lefthyphenmin for Church Slavonic).
	HyphenLeftMin = 2

	// HyphenRightMin is the minimum number of characters after the last
	// hyphenation point (polyglossia \righthyphenmin for Church Slavonic).
	HyphenRightMin = 3
)

// HyphenationReader returns an [io.Reader] over the embedded Church Slavonic
// hyphenation patterns.
func HyphenationReader() io.Reader {
	return strings.NewReader(hyphenPatterns)
}
