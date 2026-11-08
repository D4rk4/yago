package formatparse

import (
	"bytes"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagopdf"
)

func parsePDF(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	document, parsed := readPDFOrPostScript(rawURL, body)
	if !parsed {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	title := document.Title
	if title == "" {
		title = textTitle(document.Text)
	}

	return pageparse.ParsedPage{URL: rawURL, Title: title, Text: document.Text}, true
}

func readPDFOrPostScript(rawURL string, body []byte) (yagopdf.Document, bool) {
	if urlExtension(rawURL) == "ps" || bytes.HasPrefix(body, []byte("%!PS")) {
		return yagopdf.ReadPostScript(body)
	}

	return yagopdf.ReadPDF(body)
}
