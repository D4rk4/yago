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

func EncodeBase64WireForm(payload string) string {
	return tagged(wireFormBase64, Encode([]byte(payload)))
}

func tagged(tag byte, body string) string {
	return string([]byte{tag, wireFormSep}) + body
}

func DecodeWireForm(ctx context.Context, form string) (string, error) {
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
		plain, err := gzipDecompress(ctx, raw)
		if err != nil {
			return "", fmt.Errorf("inflate wire form body: %w", err)
		}
		return plain, nil
	default:
		return "", fmt.Errorf("%w: tag %q", ErrBadWireForm, form[0])
	}
}

func gzipDecompress(ctx context.Context, b []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			slog.WarnContext(
				ctx,
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
