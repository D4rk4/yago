package yacymodel

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

const (
	wireFormPlain  = 'p'
	wireFormBase64 = 'b'
	wireFormGzip   = 'z'
	wireFormSep    = '|'
)

var ErrBadWireForm = errors.New("bad wire form")

func EncodeCompactWireForm(payload string) string {
	shortest := tagged(wireFormPlain, payload)

	if b64 := tagged(wireFormBase64, Encode([]byte(payload))); len(b64) < len(shortest) {
		shortest = b64
	}

	if zipped, err := gzipCompress(payload); err == nil {
		if z := tagged(wireFormGzip, Encode(zipped)); len(z) < len(shortest) {
			shortest = z
		}
	}

	return shortest
}

func EncodeBase64WireForm(payload string) string {
	return tagged(wireFormBase64, Encode([]byte(payload)))
}

func tagged(tag byte, body string) string {
	return string([]byte{tag, wireFormSep}) + body
}

func DecodeWireForm(form string) (string, error) {
	if len(form) < 2 || form[1] != wireFormSep {
		return form, nil
	}
	body := form[2:]
	switch form[0] {
	case wireFormPlain:
		return body, nil
	case wireFormBase64:
		raw, err := Decode(body)
		if err != nil {
			return "", fmt.Errorf("decode wire form body: %w", err)
		}
		return string(raw), nil
	case wireFormGzip:
		raw, err := Decode(body)
		if err != nil {
			return "", fmt.Errorf("decode wire form body: %w", err)
		}
		plain, err := gzipDecompress(raw)
		if err != nil {
			return "", fmt.Errorf("inflate wire form body: %w", err)
		}
		return plain, nil
	default:
		return "", fmt.Errorf("%w: tag %q", ErrBadWireForm, form[0])
	}
}

func gzipCompress(s string) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := io.WriteString(w, s); err != nil {
		return nil, fmt.Errorf("gzip write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

func gzipDecompress(b []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			slog.WarnContext(
				context.Background(),
				"gzip reader close failed",
				slog.Any("error", err),
			)
		}
	}()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("gzip read: %w", err)
	}
	return string(out), nil
}
