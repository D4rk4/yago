package searchindex

import (
	"strings"
	"testing"
	"time"
)

// Facet labels are attacker-supplied text. Author, host, and file type come
// straight out of a crawled document, and the facet synopsis retains every
// label it admits for the whole search, so an unbounded label lets one page
// pin arbitrary bytes in the result set. The collector therefore drops a term
// longer than facetMaxLabel outright — it must not truncate it into a
// look-alike facet — while a label of exactly that length is still a legitimate
// author and must be counted.
func TestFacetCollectorDropsOverlongLabelsAndKeepsTheExactLimit(t *testing.T) {
	atLimit := strings.Repeat("a", facetMaxLabel)
	overLimit := strings.Repeat("b", facetMaxLabel+1)
	collector := newFacetCollector(true)
	collector.observe(facetDoc("https://at.example/x", "", atLimit, time.Time{}))
	collector.observe(facetDoc("https://over.example/x", "", overLimit, time.Time{}))

	authors := facetTermCounts(collector.groups(), "author")
	if authors[atLimit] != 1 {
		t.Fatalf("author facet = %v, want the exact-limit label counted once", authors)
	}
	if len(authors) != 1 {
		t.Fatalf("author facet = %v, want the overlong label dropped whole", authors)
	}
	for term := range authors {
		if len(term) > facetMaxLabel {
			t.Fatalf("retained facet label of %d bytes", len(term))
		}
	}
}
