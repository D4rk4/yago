package api

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"
)

const gzipContentEncoding = "gzip"

func decodeRequestBody(r *http.Request) error {
	if !strings.EqualFold(
		strings.TrimSpace(r.Header.Get("Content-Encoding")),
		gzipContentEncoding,
	) {
		return nil
	}

	body, err := gzip.NewReader(r.Body)
	if err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}

	r.Body = body
	r.Header.Del("Content-Encoding")

	return nil
}
