package formatparse

import (
	"bytes"
	"encoding/xml"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

const feedItemLimit = 200

// rssFeed covers the RSS 2.0 shape the feed parser reads.
type rssFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Description string    `xml:"description"`
		Items       []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
}

// atomFeed covers the Atom shape the feed parser reads.
type atomFeed struct {
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Links   []atomLink `xml:"link"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

// parseXMLFeeds handles the XML family: RSS and Atom feeds become one document
// whose text carries the feed items (title/summary per entry, links followable,
// the YaCy rssParser convention); other XML indexes its character data.
func parseXMLFeeds(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	if page, ok := parseRSS(rawURL, body); ok {
		return page, true
	}
	if page, ok := parseAtom(rawURL, body); ok {
		return page, true
	}

	return parseGenericXML(rawURL, body)
}

func parseRSS(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	var feed rssFeed
	if xml.Unmarshal(body, &feed) != nil || len(feed.Channel.Items) == 0 {
		return pageparse.ParsedPage{}, false
	}
	var text strings.Builder
	writeFeedLine(&text, feed.Channel.Description)
	links := make([]string, 0, len(feed.Channel.Items))
	for _, item := range clipRSSItems(feed.Channel.Items) {
		writeFeedLine(&text, item.Title)
		writeFeedLine(&text, item.Description)
		if item.Link != "" {
			links = append(links, item.Link)
		}
	}

	return feedPage(rawURL, feed.Channel.Title, text.String(), links), true
}

func parseAtom(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	var feed atomFeed
	if xml.Unmarshal(body, &feed) != nil || len(feed.Entries) == 0 {
		return pageparse.ParsedPage{}, false
	}
	var text strings.Builder
	links := make([]string, 0, len(feed.Entries))
	for _, entry := range clipAtomEntries(feed.Entries) {
		writeFeedLine(&text, entry.Title)
		writeFeedLine(&text, entry.Summary)
		writeFeedLine(&text, entry.Content)
		if href := atomEntryLink(entry); href != "" {
			links = append(links, href)
		}
	}

	return feedPage(rawURL, feed.Title, text.String(), links), true
}

// parseGenericXML indexes the character data of any other XML document.
func parseGenericXML(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if data, ok := token.(xml.CharData); ok {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				writeFeedLine(&text, trimmed)
			}
		}
	}
	content := strings.TrimSpace(text.String())
	if content == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: textTitle(content),
		Text:  content,
	}, true
}

func feedPage(rawURL, title, text string, links []string) pageparse.ParsedPage {
	text = strings.TrimSpace(text)
	if title == "" {
		title = textTitle(text)
	}

	return pageparse.ParsedPage{
		URL:             rawURL,
		Title:           strings.TrimSpace(title),
		Text:            text,
		Links:           links,
		FollowableLinks: links,
	}
}

func writeFeedLine(text *strings.Builder, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	text.WriteString(line)
	text.WriteByte('\n')
}

func clipRSSItems(items []rssItem) []rssItem {
	if len(items) > feedItemLimit {
		return items[:feedItemLimit]
	}

	return items
}

func clipAtomEntries(entries []atomEntry) []atomEntry {
	if len(entries) > feedItemLimit {
		return entries[:feedItemLimit]
	}

	return entries
}

// atomEntryLink prefers the alternate link, falling back to the first href.
func atomEntryLink(entry atomEntry) string {
	for _, link := range entry.Links {
		if link.Rel == "" || link.Rel == "alternate" {
			return link.Href
		}
	}
	if len(entry.Links) > 0 {
		return entry.Links[0].Href
	}

	return ""
}
