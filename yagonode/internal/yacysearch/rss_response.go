package yacysearch

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type rssFeed struct {
	XMLName         xml.Name   `xml:"rss"`
	Version         string     `xml:"version,attr"`
	XMLNSYaCy       string     `xml:"xmlns:yacy,attr"`
	XMLNSOpenSearch string     `xml:"xmlns:opensearch,attr"`
	XMLNSMedia      string     `xml:"xmlns:media,attr"`
	XMLNSAtom       string     `xml:"xmlns:atom,attr"`
	XMLNSDC         string     `xml:"xmlns:dc,attr"`
	XMLNSGeo        string     `xml:"xmlns:geo,attr"`
	XMLNSEvent      string     `xml:"xmlns:ev,attr"`
	Channel         rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title        string        `xml:"title"`
	Description  string        `xml:"description"`
	Link         string        `xml:"link"`
	Image        rssImage      `xml:"image"`
	StartIndex   string        `xml:"opensearch:startIndex"`
	ItemsPerPage string        `xml:"opensearch:itemsPerPage"`
	AtomLink     rssAtomLink   `xml:"atom:link"`
	Query        rssQuery      `xml:"opensearch:Query"`
	Items        []rssItem     `xml:"item"`
	Navigation   rssNavigation `xml:"yacy:navigation"`
	TotalResults string        `xml:"opensearch:totalResults"`
}

type rssImage struct {
	URL   string `xml:"url"`
	Title string `xml:"title"`
	Link  string `xml:"link"`
}

type rssAtomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr"`
}

type rssQuery struct {
	Role        string `xml:"role,attr"`
	SearchTerms string `xml:"searchTerms,attr"`
}

type rssNavigation struct {
	Facets []rssFacet `xml:"facet"`
}

type rssFacet struct {
	Name        string            `xml:"name,attr"`
	DisplayName string            `xml:"displayname,attr"`
	Type        string            `xml:"type,attr"`
	Elements    []rssFacetElement `xml:"element"`
}

type rssFacetElement struct {
	Name     string `xml:"name"`
	Count    int    `xml:"count"`
	Modifier string `xml:"modifier"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	Description string  `xml:"description"`
	PubDate     string  `xml:"pubDate"`
	Creator     string  `xml:"dc:creator"`
	Size        int     `xml:"yacy:size"`
	SizeName    string  `xml:"yacy:sizename"`
	Host        string  `xml:"yacy:host"`
	Path        string  `xml:"yacy:path"`
	File        string  `xml:"yacy:file"`
	GUID        rssGUID `xml:"guid"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

func responseRSS(r *http.Request, resp searchcore.Response) rssFeed {
	base := searchBaseURL(r)
	link := searchURL(base, resp.Request)

	return rssFeed{
		Version:         "2.0",
		XMLNSYaCy:       "http://www.yacy.net/",
		XMLNSOpenSearch: "http://a9.com/-/spec/opensearch/1.1/",
		XMLNSMedia:      "http://search.yahoo.com/mrss/",
		XMLNSAtom:       "http://www.w3.org/2005/Atom",
		XMLNSDC:         "http://purl.org/dc/elements/1.1/",
		XMLNSGeo:        "http://www.w3.org/2003/01/geo/wgs84_pos#",
		XMLNSEvent:      "http://purl.org/rss/1.0/modules/event/",
		Channel: rssChannel{
			Title:       "YaCy P2P-Search: " + resp.Request.Query,
			Description: "Search for " + resp.Request.Query,
			Link:        link,
			Image: rssImage{
				URL:   searchImageURL(r),
				Title: "Search for " + resp.Request.Query,
				Link:  link,
			},
			StartIndex:   strconv.Itoa(resp.Request.Offset),
			ItemsPerPage: strconv.Itoa(resp.Request.Limit),
			AtomLink: rssAtomLink{
				Rel:  "search",
				Href: requestBaseURL(r) + "/opensearchdescription.xml",
				Type: "application/opensearchdescription+xml",
			},
			Query: rssQuery{
				Role:        "request",
				SearchTerms: resp.Request.Query,
			},
			Items:        responseRSSItems(resp.Results),
			Navigation:   responseRSSNavigation(buildNavigation(resp.Request.Query, resp.Facets)),
			TotalResults: strconv.Itoa(resp.TotalResults),
		},
	}
}

func responseRSSItems(results []searchcore.Result) []rssItem {
	items := make([]rssItem, 0, len(results))
	for _, result := range results {
		items = append(items, rssItem{
			Title:       result.Title,
			Link:        result.URL,
			Description: result.Snippet,
			PubDate:     rssDate(result.Date),
			Creator:     result.Author,
			Size:        result.Size,
			SizeName:    sizeName(result.Size),
			Host:        result.Host,
			Path:        result.Path,
			File:        result.File,
			GUID: rssGUID{
				IsPermaLink: "false",
				Value:       result.URLHash,
			},
		})
	}

	return items
}

// responseRSSNavigation renders the shared navigation model as YaCy's
// yacy:navigation element.
func responseRSSNavigation(groups []navGroup) rssNavigation {
	facets := make([]rssFacet, 0, len(groups))
	for _, group := range groups {
		elements := make([]rssFacetElement, 0, len(group.elements))
		for _, element := range group.elements {
			elements = append(elements, rssFacetElement{
				Name:     element.name,
				Count:    element.count,
				Modifier: element.modifier,
			})
		}
		facets = append(facets, rssFacet{
			Name:        group.name,
			DisplayName: group.displayName,
			Type:        "String",
			Elements:    elements,
		})
	}

	return rssNavigation{Facets: facets}
}

func rssDate(raw string) string {
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		"20060102150405",
		"20060102",
	} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.UTC().Format(time.RFC1123Z)
		}
	}

	return raw
}
