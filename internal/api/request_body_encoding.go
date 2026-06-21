package api

import (
	"compress/gzip"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

const gzipContentEncoding = "gzip"

func decodeRequestBody(r *http.Request) error {
	encoding := strings.TrimSpace(r.Header.Get("Content-Encoding"))
	if encoding == "" {
		return nil
	}
	if !strings.EqualFold(encoding, gzipContentEncoding) {
		slog.DebugContext(
			r.Context(),
			"unsupported content encoding",
			slog.String("encoding", encoding),
		)

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
