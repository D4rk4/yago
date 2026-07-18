package formatparse

import (
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

const (
	msgMinRunLetters = 4
	msgMaxRuns       = 2000
)

// parseMSG extracts best-effort text from an Outlook .msg (CFBF/OLE2)
// container without decoding the compound file structure: readable ASCII and
// UTF-16LE runs are collected, which reliably surfaces the subject and body
// streams a search index cares about (YaCy's msg handling is text-oriented
// too). A body that yields no readable runs stays unparsed.
func parseMSG(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	runs := printableRuns(body)
	if len(runs) == 0 {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	text := strings.Join(runs, "\n")

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: textTitle(text),
		Text:  text,
	}, true
}

// printableRuns collects readable ASCII and UTF-16LE letter runs, bounded so a
// hostile container cannot balloon the extraction.
func printableRuns(body []byte) []string {
	runs := make([]string, 0, 64)
	var current strings.Builder
	letters := 0
	flush := func() {
		if letters >= msgMinRunLetters {
			runs = append(runs, strings.TrimSpace(current.String()))
		}
		current.Reset()
		letters = 0
	}
	for i := 0; i < len(body) && len(runs) < msgMaxRuns; i++ {
		b := body[i]
		// UTF-16LE ASCII appears as the byte followed by 0x00.
		if i+1 < len(body) && body[i+1] == 0 && printableASCII(b) {
			current.WriteByte(b)
			if unicode.IsLetter(rune(b)) {
				letters++
			}
			i++

			continue
		}
		if printableASCII(b) {
			current.WriteByte(b)
			if unicode.IsLetter(rune(b)) {
				letters++
			}

			continue
		}
		flush()
	}
	flush()

	return runs
}

func printableASCII(b byte) bool {
	return b >= 0x20 && b < 0x7f
}
