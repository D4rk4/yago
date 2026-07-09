package formatparse

import (
	"strings"
	"unicode/utf8"
)

const textTitleRuneCap = 80

// parseTextFamily handles the text family: txt and tex index as raw text, csv
// as its cell text, RTF through its control-word walker, and Outlook MSG
// through best-effort readable-run extraction.
func parseTextFamily(rawURL, _ string, body []byte) (Document, bool) {
	switch urlExtension(rawURL) {
	case "rtf":
		return parseRTF(rawURL, body)
	case "msg":
		return parseMSG(rawURL, body)
	}
	text := strings.TrimSpace(strings.ToValidUTF8(string(body), ""))
	if text == "" {
		return Document{URL: rawURL}, false
	}

	return Document{
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
