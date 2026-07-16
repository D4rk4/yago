package querymatch

import "testing"

func TestNextBoundedTerm(t *testing.T) {
	text := "K Capital node.js v0.0.9 node.js"
	start, end, found := NextBoundedTerm(text, "NODE.JS", 0)
	if !found || text[start:end] != "node.js" {
		t.Fatalf("first bounded term = %d:%d/%v", start, end, found)
	}
	start, end, found = NextBoundedTerm(text, "node.js", end)
	if !found || text[start:end] != "node.js" {
		t.Fatalf("second bounded term = %d:%d/%v", start, end, found)
	}
	if _, _, found := NextBoundedTerm(text, "api", 0); found {
		t.Fatal("embedded Latin term passed word boundaries")
	}
	if _, _, found := NextBoundedTerm(text, "v0.0.9", 0); !found {
		t.Fatal("version identifier was not found")
	}
}

func TestNextLiteralTermPreservesOriginalByteOffsets(t *testing.T) {
	text := "K東京タワー"
	start, end, found := NextLiteralTerm(text, "東京", 0)
	if !found || text[start:end] != "東京" {
		t.Fatalf("literal term = %d:%d/%v", start, end, found)
	}
}

func TestNextBoundedTermAtTextStart(t *testing.T) {
	text := "node.js guide"
	start, end, found := NextBoundedTerm(text, "node.js", 0)
	if !found || start != 0 || text[start:end] != "node.js" {
		t.Fatalf("bounded term = %d:%d/%v", start, end, found)
	}
}

func TestNextBoundedTermRejectsInvalidInput(t *testing.T) {
	for _, input := range []struct {
		text string
		term string
		from int
	}{
		{text: "", term: "term", from: 0},
		{text: "text", term: " ", from: 0},
		{text: "text", term: "text", from: -1},
		{text: "text", term: "text", from: 5},
	} {
		if _, _, found := NextBoundedTerm(input.text, input.term, input.from); found {
			t.Fatalf("invalid input matched: %#v", input)
		}
	}
	if _, _, found := NextLiteralTerm("text", "term", 5); found {
		t.Fatal("literal term accepted an out-of-range offset")
	}
}

func TestTermContainsWordSeparator(t *testing.T) {
	if !TermContainsWordSeparator("node.js") {
		t.Fatal("punctuated term separator was not found")
	}
	if TermContainsWordSeparator("полномочия") {
		t.Fatal("ordinary word was treated as punctuated")
	}
}
