package pdfrender

import (
	"github.com/boxesandglue/boxesandglue/backend/lang"

	"github.com/grisha/slavonic-render/cu"
)

// loadChurchSlavonicHyphenation loads Church Slavonic hyphenation patterns.
// The patterns are embedded in the binary at compile time.
func loadChurchSlavonicHyphenation() (*lang.Lang, error) {
	l, err := lang.NewFromReader(cu.HyphenationReader())
	if err != nil {
		return nil, err
	}
	l.Name = "cu"
	l.Lefthyphenmin = cu.HyphenLeftMin
	l.Righthyphenmin = cu.HyphenRightMin
	return l, nil
}
