package searchindex

import (
	"path"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	facetTermLimit = 8
	facetMaxLabel  = 48
)

// FacetGroup is one facet dimension with its most frequent terms among every
// document that matched the query and its filters — counted after the
// post-retrieval filters, so the numbers agree with what paging can reach.
type FacetGroup struct {
	Name  string
	Terms []FacetTerm
}

type FacetTerm struct {
	Term  string
	Count int
}

// facetCollector tallies facet dimensions over matching documents.
type facetCollector struct {
	counts map[string]map[string]int
}

func newFacetCollector(enabled bool) *facetCollector {
	if !enabled {
		return nil
	}

	return &facetCollector{counts: map[string]map[string]int{}}
}

func (c *facetCollector) observe(doc documentstore.Document) {
	if c == nil {
		return
	}
	c.add("host", documentHost(doc))
	c.add("filetype", documentFileType(doc))
	c.add("language", strings.ToLower(doc.Language))
	c.add("author", doc.Metadata["author"])
	c.add("protocol", documentProtocol(doc))
	c.add("month", documentMonth(doc))
}

func (c *facetCollector) add(name, term string) {
	term = strings.TrimSpace(term)
	if term == "" || len(term) > facetMaxLabel {
		return
	}
	group := c.counts[name]
	if group == nil {
		group = map[string]int{}
		c.counts[name] = group
	}
	group[term]++
}

// groups renders the tallied dimensions in a stable order with their most
// frequent terms first; empty dimensions are omitted.
func (c *facetCollector) groups() []FacetGroup {
	if c == nil {
		return nil
	}
	out := make([]FacetGroup, 0, len(c.counts))
	for _, name := range []string{"host", "filetype", "language", "author", "protocol", "month"} {
		group := c.counts[name]
		if len(group) == 0 {
			continue
		}
		terms := make([]FacetTerm, 0, len(group))
		for term, count := range group {
			terms = append(terms, FacetTerm{Term: term, Count: count})
		}
		sort.Slice(terms, func(i, j int) bool {
			if terms[i].Count != terms[j].Count {
				return terms[i].Count > terms[j].Count
			}

			return terms[i].Term < terms[j].Term
		})
		if len(terms) > facetTermLimit {
			terms = terms[:facetTermLimit]
		}
		out = append(out, FacetGroup{Name: name, Terms: terms})
	}

	return out
}

func documentFileType(doc documentstore.Document) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(urlPathOf(documentURL(doc)))), ".")
	if len(ext) > 5 {
		return ""
	}

	return ext
}

func documentProtocol(doc documentstore.Document) string {
	url := documentURL(doc)
	scheme, _, found := strings.Cut(url, "://")
	if !found {
		return ""
	}

	return strings.ToLower(scheme)
}

func documentMonth(doc documentstore.Document) string {
	when := documentTime(doc)
	if when.IsZero() {
		return ""
	}

	return when.UTC().Format("2006-01")
}

func urlPathOf(rawURL string) string {
	_, rest, found := strings.Cut(rawURL, "://")
	if !found {
		return ""
	}
	_, urlPath, found := strings.Cut(rest, "/")
	if !found {
		return ""
	}
	if question := strings.IndexByte(urlPath, '?'); question >= 0 {
		urlPath = urlPath[:question]
	}

	return urlPath
}
