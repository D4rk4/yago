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
	body, err := f.readBoundedBody(limited, rawURL)
	if err != nil {
		return fetchedDocument{}, err
	}

	return fetchedDocument{plainText: strings.TrimSpace(string(body))}, nil
}

func (f *Fetcher) readBoundedBody(limited *io.LimitedReader, rawURL string) ([]byte, error) {
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	if limited.N == 0 {
		return nil, f.responseTooLarge(rawURL)
	}

	return body, nil
}
