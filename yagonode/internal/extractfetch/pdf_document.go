package extractfetch

import (
	"context"
	"fmt"
	"io"

	"github.com/D4rk4/yago/yagopdf"
)

func isPDF(contentType string) bool {
	return mediaType(contentType) == "application/pdf"
}

func (f *Fetcher) readPDF(
	ctx context.Context,
	limited *io.LimitedReader,
	rawURL string,
) (fetchedDocument, error) {
	body, err := f.readBoundedBody(limited, rawURL)
	if err != nil {
		return fetchedDocument{}, err
	}
	document, err := yagopdf.ReadPDFContext(ctx, body)
	if err != nil {
		return fetchedDocument{}, fmt.Errorf("parse PDF %s: %w", rawURL, err)
	}

	return fetchedDocument{title: document.Title, plainText: document.Text}, nil
}
