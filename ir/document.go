package ir

import (
	"encoding/json"
	"fmt"
)

// BlockKind identifies the type of a document block.
type BlockKind int

const (
	BlockParagraph     BlockKind = iota // Regular body paragraph
	BlockCentered                       // Centered paragraph (| prefix)
	BlockDropCap                        // Drop cap paragraph (^ prefix)
	BlockHeading1                       // Title (# heading)
	BlockHeading2                       // Section heading (## heading)
	BlockImage                          // Ornament/image
	BlockRunningHeader                  // Running header change
	BlockPageBreak                      // Force new page
	BlockThematicBreak                  // Heading-level vertical space
	BlockPageContextOp                  // Page context directive
	BlockAnchor                         // Anchor directive
)

var blockKindNames = [...]string{
	BlockParagraph:     "paragraph",
	BlockCentered:      "centered",
	BlockDropCap:       "dropcap",
	BlockHeading1:      "heading1",
	BlockHeading2:      "heading2",
	BlockImage:         "image",
	BlockRunningHeader: "running-header",
	BlockPageBreak:     "page-break",
	BlockThematicBreak: "thematic-break",
	BlockPageContextOp: "page-context-op",
	BlockAnchor:        "anchor",
}

var blockKindFromName map[string]BlockKind

func init() {
	blockKindFromName = make(map[string]BlockKind, len(blockKindNames))
	for i, name := range blockKindNames {
		blockKindFromName[name] = BlockKind(i)
	}
}

func (k BlockKind) String() string {
	if int(k) < len(blockKindNames) {
		return blockKindNames[k]
	}
	return fmt.Sprintf("BlockKind(%d)", k)
}

func (k BlockKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

func (k *BlockKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	kind, ok := blockKindFromName[s]
	if !ok {
		return fmt.Errorf("UnmarshalJSON: unknown BlockKind %q", s)
	}
	*k = kind
	return nil
}

// Block represents a single block-level element in a document.
type Block struct {
	Kind     BlockKind    `json:"kind"`
	Content  []Span       `json:"content,omitempty"`
	Indented bool         `json:"indented,omitempty"`
	Minor    bool         `json:"minor,omitempty"`
	Notes    []MarginNote `json:"notes,omitempty"`
	Text     string       `json:"text,omitempty"`
	Key      string       `json:"key,omitempty"`
	Value    string       `json:"value,omitempty"`
}

// FootnoteDef holds a footnote definition with its display number and content.
type FootnoteDef struct {
	ID      string `json:"id"`      // original markdown ID (matching key)
	Num     int    `json:"num"`     // display number (1, 2, 3...)
	Content []Span `json:"content"` // footnote body with inline formatting
}

// Document is a sequence of blocks representing a complete rendered document.
type Document struct {
	Blocks    []Block       `json:"blocks"`
	Footnotes []FootnoteDef `json:"footnotes,omitempty"`
}
