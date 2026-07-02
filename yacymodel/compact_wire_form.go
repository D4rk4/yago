package yacymodel

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
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

	if z := tagged(wireFormGzip, Encode(gzipCompress(payload))); len(z) < len(shortest) {
		shortest = z
	}

	return shortest
}

func EncodeBase64WireForm(payload string) string {
	return tagged(wireFormBase64, Encode([]byte(payload)))
}

func tagged(tag byte, body string) string {
	return string([]byte{tag, wireFormSep}) + body
}

func DecodeWireForm(_ context.Context, form string) (string, error) {
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

func gzipCompress(s string) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	return buf.Bytes()
}

func gzipDecompress(b []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = r.Close() }()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("gzip read: %w", err)
	}
	return string(out), nil
}
