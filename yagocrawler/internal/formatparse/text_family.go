package formatparse

import (
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

const textTitleRuneCap = 80

// parseTextFamily handles the plain-text members of the text family today:
// txt and tex index as raw text, csv as its cell text. RTF and MSG carry
// markup/container structure a plain read would garble, so they report
// unparsable until their extractors land.
func parseTextFamily(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	switch urlExtension(rawURL) {
	case "rtf", "msg":
		return pageparse.ParsedPage{URL: rawURL}, false
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
