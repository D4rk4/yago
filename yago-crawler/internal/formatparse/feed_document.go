package formatparse

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

// maxFeedItemCandidates bounds the number of completed item candidates a feed
// pass may examine before stopping bounded work.
const maxFeedItemCandidates = 200

// feedDiscovery carries the outcome of one bounded token pass over a feed body:
// whether the root recognized a feed shape, the links completed from closed
// entries before any fault, whether the document closed cleanly, and the parse
// error when the document did not.
type feedDiscovery struct {
	recognized bool
	feedTitle  string
	text       *strings.Builder
	links      []string
	complete   bool
	err        error
}

// feedLink is one <link> element inside a feed entry: its relationship, target,
// body text, and the base the target resolves against.
type feedLink struct {
	rel  string
	href string
	text string
	base string
}

// feedGUID is one <guid> element: its value and isPermaLink attribute.
type feedGUID struct {
	value string
	perma string
	set   bool
}

// feedEntry is one item or entry whose text and links are collected during the
// single token pass over the feed body.
type feedEntry struct {
	title       string
	description string
	summary     string
	content     string
	links       []feedLink
	guid        feedGUID
	base        string
}

// feedCursor tracks the entry under collection and the child element whose
// character data the next CharData belongs to.
type feedCursor struct {
	entry     *feedEntry
	active    string
	feedBase  string
	feedTitle string
}

// parseXMLFeeds runs one bounded token pass over the feed body. It routes a
// recognized sitemap to its own family, and otherwise feeds the tokens to the
// feed discovery. An unrecognized body falls through to generic XML. A complete
// feed reports parsed=true; a broken recognized feed keeps the links from closed
// entries and reports parsed=false.
func parseXMLFeeds(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	if page, parsed, sitemap := parseSitemapXML(rawURL, body); sitemap {
		return page, parsed
	}

	discovery := discoverFeed(body)
	if !discovery.recognized {
		return parseGenericXML(rawURL, body)
	}

	return feedPage(
		rawURL,
		discovery.feedTitle,
		discovery.text.String(),
		discovery.links,
	), discovery.complete
}

// discoverFeed consumes the token stream once, collecting feed links and entry
// text. Text feeds the active child field; a link or guid starts an open
// element; an entry or channel/feed close finalizes the current entry; any
// terminal token ends the pass. io.EOF closes cleanly; any other error leaves
// the pass incomplete while retaining links from fully closed entries. At most
// maxFeedItemCandidates items contribute links per feed.
func discoverFeed(body []byte) feedDiscovery {
	result := feedDiscovery{complete: true, text: &strings.Builder{}}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	cursor := feedCursor{}
	closedItems := 0

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			result.complete = false
			result.err = err
			break
		}

		switch value := token.(type) {
		case xml.CharData:
			cursor.charData(string(value), result.text)
		case xml.ProcInst, xml.Comment:
		case xml.StartElement:
			if !result.recognized {
				result.recognized = isFeedRoot(value.Name)
			}
			cursor.openElement(value, result.text, &result.links)
		case xml.EndElement:
			closedItems += cursor.closeElement(value, result.text, &result.links)
		}

		if closedItems >= maxFeedItemCandidates {
			break
		}
	}

	result.feedTitle = cursor.feedTitle
	return result
}

// charData attributes character data to the active child field. At feed level
// (no open entry) the channel description is written directly and the channel
// title is captured; inside an entry it fills the entry fields or the open link
// body.
func (cursor *feedCursor) charData(value string, text *strings.Builder) {
	if cursor.entry == nil {
		switch cursor.active {
		case "title":
			cursor.feedTitle = value
		case "description":
			writeFeedLine(text, value)
		}
		return
	}

	switch cursor.active {
	case "title":
		cursor.entry.title = value
	case "description":
		cursor.entry.description = value
	case "summary":
		cursor.entry.summary = value
	case "content":
		cursor.entry.content = value
	case "link":
		if len(cursor.entry.links) > 0 {
			cursor.entry.links[len(cursor.entry.links)-1].text = value
		}
	case "guid":
		cursor.entry.guid.value += value
	}
}

// openElement handles a start element for one bounded token pass.
func (cursor *feedCursor) openElement(
	value xml.StartElement,
	text *strings.Builder,
	links *[]string,
) {
	switch value.Name.Local {
	case "rss", "channel", "feed":
		if base := attrOf(value, "xml:base"); base != "" {
			cursor.feedBase = base
		}
	case "item", "entry":
		flushEntry(cursor.entry, text, links)
		cursor.entry = &feedEntry{
			base: resolveBase(cursor.feedBase, attrOf(value, "xml:base")),
		}
		cursor.active = ""
	case "link":
		if cursor.entry != nil {
			cursor.entry.links = append(cursor.entry.links, feedLink{
				rel:  attrOf(value, "rel"),
				href: attrOf(value, "href"),
				base: resolveBase(cursor.entry.base, attrOf(value, "xml:base")),
			})
			cursor.active = "link"
		}
	case "guid":
		if cursor.entry != nil {
			cursor.entry.guid = feedGUID{
				perma: attrOf(value, "isPermaLink"),
				set:   true,
			}
			cursor.active = "guid"
		}
	case "title", "description", "summary", "content":
		cursor.active = value.Name.Local
	}
}

// closeElement handles an end element and returns whether an item closed.
func (cursor *feedCursor) closeElement(
	value xml.EndElement,
	text *strings.Builder,
	links *[]string,
) int {
	closed := 0
	switch value.Name.Local {
	case "channel", "feed", "rss":
		flushEntry(cursor.entry, text, links)
		cursor.entry = nil
		cursor.active = ""
	case "item", "entry":
		flushEntry(cursor.entry, text, links)
		cursor.entry = nil
		cursor.active = ""
		closed = 1
	case "title", "description", "summary", "content", "link", "guid":
		cursor.active = ""
	}
	return closed
}

// flushEntry resolves the article address for the entry under collection,
// appends it to the result links when one exists, and records the entry text.
func flushEntry(entry *feedEntry, text *strings.Builder, links *[]string) {
	if entry == nil {
		return
	}
	if article := selectItemLink(entry); article != "" {
		*links = append(*links, article)
	}

	writeFeedLine(text, textBuffer(entry.title, entry.description,
		entry.summary, entry.content))
}

// textBuffer joins the entry fields the feed parser preserves into one line.
func textBuffer(parts ...string) string {
	text := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			text += part + "\n"
		}
	}

	return text
}

// rdfNamespace is the RDF syntax namespace that names the RSS1/RDF root.
const rdfNamespace = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

// isFeedRoot reports whether an element names a feed root: rss for RSS2,
// feed for Atom (including Atom 0.3), and rdf:RDF for RSS1/RDF.
func isFeedRoot(name xml.Name) bool {
	switch name.Local {
	case "rss", "feed":
		return true
	case "RDF":
		return name.Space == rdfNamespace
	}

	return false
}

// attrOf returns the value of the attribute named name on the start element,
// joining the namespace prefix on the local name (e.g. xml:base) as the decoder
// reports it.
func attrOf(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == name || attr.Name.Local == strings.TrimPrefix(name, "xml:") {
			return attr.Value
		}
	}

	return ""
}
