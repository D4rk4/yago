package formatparse

import (
	"bytes"
	"encoding/xml"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	feedEntryCandidateLimit = 200
	rdfSyntaxNamespace      = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	atomNamespace           = "http://www.w3.org/2005/Atom"
	atomDraftNamespace      = "http://purl.org/atom/ns#"
)

type feedLevel int

const (
	feedRootLevel feedLevel = iota
	feedChannelLevel
)

type feedElementRole int

const (
	unreadFeedElement feedElementRole = iota
	feedChannelElement
	feedTitleElement
	feedDescriptionElement
	feedEntryElement
)

type feedFormat struct {
	metadataLevel   feedLevel
	entryLevel      feedLevel
	entryName       string
	descriptionName string
	entryTextNames  []string
	linkInHref      bool
}

var (
	rssFeedFormat = feedFormat{
		metadataLevel:   feedChannelLevel,
		entryLevel:      feedChannelLevel,
		entryName:       "item",
		descriptionName: "description",
		entryTextNames:  []string{"title", "description"},
	}
	rdfFeedFormat = feedFormat{
		metadataLevel:   feedChannelLevel,
		entryLevel:      feedRootLevel,
		entryName:       "item",
		descriptionName: "description",
		entryTextNames:  []string{"title", "description"},
	}
	atomFeedFormat = feedFormat{
		metadataLevel:  feedRootLevel,
		entryLevel:     feedRootLevel,
		entryName:      "entry",
		entryTextNames: []string{"title", "summary", "content"},
		linkInHref:     true,
	}
)

type feedDiscovery struct {
	recognized bool
	complete   bool
	title      string
	text       string
	links      []string
}

type feedEntry struct {
	base            url.URL
	textFields      []boundedFeedText
	articleURL      string
	guid            boundedFeedText
	guidIsPermalink bool
}

type feedReader struct {
	decoder       *xml.Decoder
	format        feedFormat
	title         boundedFeedText
	description   boundedFeedText
	entryLines    strings.Builder
	links         []string
	closedEntries int
}

type boundedFeedText struct {
	content []byte
	room    int
	clipped bool
}

func discoverFeed(documentURL string, body []byte) feedDiscovery {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	root := feedRootElement(decoder)
	format, recognized := feedFormatOf(root.Name)
	if !recognized {
		return feedDiscovery{}
	}
	reader := &feedReader{decoder: decoder, format: format}
	closed := reader.readContainer(root, feedRootLevel, resolveBase(url.URL{}, documentURL))

	return reader.discovery(closed)
}

func feedRootElement(decoder *xml.Decoder) xml.StartElement {
	for {
		token, err := decoder.Token()
		if err != nil {
			return xml.StartElement{}
		}
		if root, isElement := token.(xml.StartElement); isElement {
			return root
		}
	}
}

func feedFormatOf(root xml.Name) (feedFormat, bool) {
	switch {
	case root.Local == "rss" && root.Space == "":
		return rssFeedFormat, true
	case root.Local == "RDF" && root.Space == rdfSyntaxNamespace:
		return rdfFeedFormat, true
	case root.Local == "feed" && (root.Space == atomNamespace || root.Space == atomDraftNamespace):
		return atomFeedFormat, true
	}

	return feedFormat{}, false
}

func (format feedFormat) roleOf(level feedLevel, name string) feedElementRole {
	switch {
	case level == feedRootLevel && name == "channel":
		return feedChannelElement
	case level == format.entryLevel && name == format.entryName:
		return feedEntryElement
	case level == format.metadataLevel && name == "title":
		return feedTitleElement
	case level == format.metadataLevel && name == format.descriptionName:
		return feedDescriptionElement
	}

	return unreadFeedElement
}

func (reader *feedReader) readContainer(
	container xml.StartElement,
	level feedLevel,
	inherited url.URL,
) bool {
	base := withXMLBase(inherited, container)

	return reader.readChildren(func(child xml.StartElement) bool {
		return reader.readContainerChild(child, level, base)
	})
}

func (reader *feedReader) readContainerChild(
	child xml.StartElement,
	level feedLevel,
	base url.URL,
) bool {
	switch reader.format.roleOf(level, child.Name.Local) {
	case feedChannelElement:
		return reader.readContainer(child, feedChannelLevel, base)
	case feedTitleElement:
		reader.title = newBoundedFeedText(yagocrawlcontract.MaximumDocumentTextBytes)

		return reader.readDirectText(&reader.title)
	case feedDescriptionElement:
		reader.description = newBoundedFeedText(yagocrawlcontract.MaximumDocumentTextBytes)

		return reader.readDirectText(&reader.description)
	case feedEntryElement:
		return reader.readEntry(child, base)
	default:
		return reader.skipElement()
	}
}

func (reader *feedReader) readEntry(start xml.StartElement, inherited url.URL) bool {
	if reader.closedEntries >= feedEntryCandidateLimit {
		return reader.skipElement()
	}
	entry := feedEntry{
		base:       withXMLBase(inherited, start),
		textFields: make([]boundedFeedText, len(reader.format.entryTextNames)),
	}
	closed := reader.readChildren(func(child xml.StartElement) bool {
		return reader.readEntryChild(&entry, child)
	})
	if closed {
		reader.closeEntry(&entry)
	}

	return closed
}

func (reader *feedReader) readEntryChild(entry *feedEntry, child xml.StartElement) bool {
	if field := slices.Index(reader.format.entryTextNames, child.Name.Local); field >= 0 {
		entry.textFields[field] = newBoundedFeedText(yagocrawlcontract.MaximumDocumentTextBytes)

		return reader.readDirectText(&entry.textFields[field])
	}
	switch child.Name.Local {
	case "link":
		return reader.readEntryLink(entry, child)
	case "guid":
		entry.guid = newBoundedFeedText(yagocrawlcontract.MaximumCrawlURLBytes)
		entry.guidIsPermalink = attributeValue(child, permalinkAttribute) != "false"

		return reader.readDirectText(&entry.guid)
	default:
		return reader.skipElement()
	}
}

func (reader *feedReader) readEntryLink(entry *feedEntry, link xml.StartElement) bool {
	linkText := newBoundedFeedText(yagocrawlcontract.MaximumCrawlURLBytes)
	closed := reader.readDirectText(&linkText)
	target := linkText.completeText()
	if reader.format.linkInHref {
		target = attributeValue(link, hrefAttribute)
	}
	if entry.articleURL == "" {
		entry.articleURL = entryLinkTarget(
			withXMLBase(entry.base, link),
			attributeValue(link, relAttribute),
			target,
		)
	}

	return closed
}

func (reader *feedReader) closeEntry(entry *feedEntry) {
	for index := range entry.textFields {
		writeFeedLine(&reader.entryLines, entry.textFields[index].String())
	}
	if article := entry.article(); article != "" {
		reader.links = append(reader.links, article)
	}
	reader.closedEntries++
}

func (reader *feedReader) readChildren(readChild func(xml.StartElement) bool) bool {
	for {
		token, err := reader.decoder.Token()
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.StartElement:
			if !readChild(value) {
				return false
			}
		case xml.EndElement:
			return true
		}
	}
}

func (reader *feedReader) readDirectText(text *boundedFeedText) bool {
	nestedElements := 0
	for {
		token, err := reader.decoder.Token()
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.CharData:
			if nestedElements == 0 {
				text.append(value)
			}
		case xml.StartElement:
			nestedElements++
		case xml.EndElement:
			if nestedElements == 0 {
				return true
			}
			nestedElements--
		}
	}
}

func (reader *feedReader) skipElement() bool {
	return reader.decoder.Skip() == nil
}

func (reader *feedReader) discovery(complete bool) feedDiscovery {
	var text strings.Builder
	writeFeedLine(&text, reader.description.String())
	text.WriteString(reader.entryLines.String())

	return feedDiscovery{
		recognized: true,
		complete:   complete,
		title:      reader.title.String(),
		text:       text.String(),
		links:      reader.links,
	}
}

func newBoundedFeedText(limit int) boundedFeedText {
	return boundedFeedText{room: limit}
}

func (text *boundedFeedText) append(chunk []byte) {
	if len(chunk) > text.room {
		cut := text.room
		for cut > 0 && !utf8.RuneStart(chunk[cut]) {
			cut--
		}
		chunk = chunk[:cut]
		text.room = cut
		text.clipped = true
	}
	text.content = append(text.content, chunk...)
	text.room -= len(chunk)
}

func (text *boundedFeedText) String() string {
	return string(text.content)
}

func (text *boundedFeedText) completeText() string {
	if text.clipped {
		return ""
	}

	return string(text.content)
}
