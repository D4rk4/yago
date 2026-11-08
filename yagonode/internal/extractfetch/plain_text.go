package extractfetch

import (
	"fmt"
	"io"
	"strings"
)

func isPlainText(contentType string) bool {
	media := mediaType(contentType)

	return media == "text/plain" || media == "text/markdown"
}

func (f *Fetcher) readPlainText(limited *io.LimitedReader, rawURL string) (fetchedDocument, error) {
	body, err := io.ReadAll(limited)
	if err != nil {
		return fetchedDocument{}, fmt.Errorf("read %s: %w", rawURL, err)
	}
	if limited.N == 0 {
		return fetchedDocument{}, f.responseTooLarge(rawURL)
	}

	return fetchedDocument{plainText: strings.TrimSpace(string(body))}, nil
}
