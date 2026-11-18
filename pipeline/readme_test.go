package pipeline

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/grisha/slavonic-render/htmlrender"
	"github.com/grisha/slavonic-render/markup"
)

// The README follows examples/psalm22.md through every stage, and its
// listings are the stages' own output. This test holds them to it: when a
// stage writes something else, the README's listing is to be made again.
func TestReadmeExample(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../examples/psalm22.md")
	if err != nil {
		t.Fatal(err)
	}
	expanded, frontMatter, err := ExpandFile("../examples/psalm22.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := markup.Parse(expanded)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	if err := htmlrender.Render(&page, doc, frontMatter); err != nil {
		t.Fatal(err)
	}

	// The source is listed twice, at the top and in "How it works": counting
	// the listings keeps one copy from falling behind the other.
	for _, stage := range []struct {
		name, output string
		times        int
	}{
		{"the source", string(source), 2},
		{"the expanded Markdown (-dump-md)", expanded, 1},
		{"the parsed document (-dump-ir)", string(dump), 1},
		{"the HTML", page.String(), 1},
	} {
		listing := "\n" + strings.TrimSpace(stage.output) + "\n```\n"
		if n := strings.Count(string(readme), listing); n != stage.times {
			t.Errorf("README.md lists %s as it is now %d times, not %d:\n%s", stage.name, n, stage.times, stage.output)
		}
	}
}
