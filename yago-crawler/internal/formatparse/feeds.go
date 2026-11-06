package formatparse

import (
	"bytes"
	"encoding/xml"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

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
