package formatparse

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagopdf"
)

const pdfReadingTimeLimit = 5 * time.Second

func readPDFOrPostScriptWithin(
	limit time.Duration,
	rawURL string,
	body []byte,
) (yagopdf.Document, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	read := yagopdf.ReadPDFContext
	if urlExtension(rawURL) == "ps" || bytes.HasPrefix(body, []byte("%!PS")) {
		read = yagopdf.ReadPostScriptContext
	}
	document, err := read(ctx, body)
	if err != nil {
		return yagopdf.Document{}, fmt.Errorf("read %s: %w", rawURL, err)
	}

	return document, nil
}
