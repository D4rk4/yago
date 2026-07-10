package formatparse

import (
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

const textTitleRuneCap = 80

// parseTextFamily handles the text family: txt and tex index as raw text, csv
// as its cell text, RTF through its control-word walker, and Outlook MSG
// through best-effort readable-run extraction.
func parseTextFamily(rawURL, contentType string, body []byte) (pageparse.ParsedPage, bool) {
	ext := urlExtension(rawURL)
	mime := mimeType(contentType)
	switch {
	case ext == "rtf" || mime == "application/rtf" || mime == "text/rtf":
		return parseRTF(rawURL, body)
	case ext == "msg" || mime == "application/vnd.ms-outlook":
		return parseMSG(rawURL, body)
	}
	text := strings.TrimSpace(strings.ToValidUTF8(string(body), ""))
	if text == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: textTitle(text),
		Text:  text,
	}, true
}

// textTitle uses the first line, bounded, as the document title.
func textTitle(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > textTitleRuneCap {
		runes := []rune(line)

		return string(runes[:textTitleRuneCap])
	}

	return line
}
