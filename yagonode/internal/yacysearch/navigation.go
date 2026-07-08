package yacysearch

import (
	"net/url"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// facetNavNames maps the local facet-group names onto YaCy's navigator ids, so a
// client that keys on the wire facetname sees the names it expects.
var facetNavNames = map[string]string{
	"host":     "hosts",
	"filetype": "filetypes",
	"language": "languages",
	"author":   "authors",
	"protocol": "protocols",
	"month":    "dates",
}

// facetDisplayNames is the human label each navigator carries.
var facetDisplayNames = map[string]string{
	"host":     "Provider",
	"filetype": "Filetype",
	"language": "Language",
	"author":   "Author",
	"protocol": "Protocol",
	"month":    "Date",
}

// facetModifiers maps a facet dimension onto the query operator a value adds when
// selected; dimensions without an operator render as plain counts. The operators
// round-trip through ParseTextQuery so a client's refined query parses back.
var facetModifiers = map[string]string{
	"host":     "site:",
	"filetype": "filetype:",
	"language": "language:",
	"author":   "author:",
}

// navGroup is the surface-neutral navigation model the JSON and RSS responders
// each render into their own wire shape.
type navGroup struct {
	name        string
	displayName string
	elements    []navElement
}

type navElement struct {
	name     string
	count    int
	modifier string
	url      string
}

// buildNavigation turns the local facet groups into the shared navigation model,
// attaching a refine modifier and URL to every value whose dimension has one.
func buildNavigation(query string, facets []searchcore.FacetGroup) []navGroup {
	groups := make([]navGroup, 0, len(facets))
	for _, facet := range facets {
		operator := facetModifiers[facet.Name]
		elements := make([]navElement, 0, len(facet.Terms))
		for _, term := range facet.Terms {
			element := navElement{name: term.Term, count: term.Count}
			if operator != "" {
				element.modifier = operator + term.Term
				element.url = "?query=" + url.QueryEscape(query+" "+element.modifier)
			}
			elements = append(elements, element)
		}
		groups = append(groups, navGroup{
			name:        facetNavName(facet.Name),
			displayName: facetDisplayNames[facet.Name],
			elements:    elements,
		})
	}

	return groups
}

// facetNavName returns the wire navigator id for a facet dimension, falling back
// to the dimension name for anything without a mapping.
func facetNavName(name string) string {
	if wire, ok := facetNavNames[name]; ok {
		return wire
	}

	return name
}
