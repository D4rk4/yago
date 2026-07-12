package searchindex

import (
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/filetypeclass"
)

const (
	facetTermLimit       = 8
	facetSynopsisEntries = 256
	facetMaxLabel        = 48
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
	counts map[string]*facetFrequencySynopsis
}

func newFacetCollector(enabled bool) *facetCollector {
	if !enabled {
		return nil
	}

	return &facetCollector{counts: map[string]*facetFrequencySynopsis{}}
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
		group = newFacetFrequencySynopsis(facetSynopsisEntries)
		c.counts[name] = group
	}
	group.observe(term)
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
		if group == nil || len(group.entries) == 0 {
			continue
		}
		terms := group.terms()
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
	return filetypeclass.Canonical(documentURL(doc), doc.ContentType)
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
