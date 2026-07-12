package yagomodel

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

	maximumInflatedWireFormBytes int64 = 4 << 20
)

var (
	ErrBadWireForm              = errors.New("bad wire form")
	errInflatedWireFormTooLarge = errors.New("inflated wire form too large")
)

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

func DecodeWireForm(ctx context.Context, form string) (string, error) {
	return DecodeWireFormWithLimit(ctx, form, maximumInflatedWireFormBytes)
}

func DecodeWireFormWithLimit(
	_ context.Context,
	form string,
	maximumBytes int64,
) (string, error) {
	if len(form) < 2 || form[1] != wireFormSep {
		if err := wireFormSizeError(int64(len(form)), maximumBytes); err != nil {
			return "", err
		}
		return form, nil
	}
	body := form[2:]
	switch form[0] {
	case wireFormPlain:
		if err := wireFormSizeError(int64(len(body)), maximumBytes); err != nil {
			return "", err
		}
		return body, nil
	case wireFormBase64:
		if err := encodedWireFormSizeError(body, maximumBytes); err != nil {
			return "", err
		}
		raw, _ := Decode(body)
		return string(raw), nil
	case wireFormGzip:
		if err := encodedWireFormSizeError(body, maximumBytes); err != nil {
			return "", err
		}
		raw, _ := Decode(body)
		plain, err := gzipDecompress(raw, maximumBytes)
		if err != nil {
			return "", fmt.Errorf("inflate wire form body: %w", err)
		}
		return plain, nil
	default:
		return "", fmt.Errorf("%w: tag %q", ErrBadWireForm, form[0])
	}
}

func encodedWireFormSizeError(body string, maximumBytes int64) error {
	decodedBytes, err := decodedWireFormBodyBytes(body)
	if err != nil {
		return fmt.Errorf("decode wire form body: %w", err)
	}

	return wireFormSizeError(decodedBytes, maximumBytes)
}

func decodedWireFormBodyBytes(body string) (int64, error) {
	var symbols int64
	for position := range len(body) {
		if body[position] == '\n' {
			continue
		}
		if decodeTable[body[position]] < 0 {
			return 0, fmt.Errorf("%w: %q", ErrInvalidBase64, body[position])
		}
		symbols++
	}

	return symbols/4*3 + symbols%4*6/8, nil
}

func wireFormSizeError(size, maximumBytes int64) error {
	if size <= maximumBytes {
		return nil
	}

	return fmt.Errorf(
		"%w: maximum %d bytes",
		errInflatedWireFormTooLarge,
		maximumBytes,
	)
}

func gzipCompress(s string) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	return buf.Bytes()
}

func gzipDecompress(b []byte, maximumBytes int64) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = r.Close() }()
	out, err := io.ReadAll(io.LimitReader(r, maximumBytes+1))
	if err != nil {
		return "", fmt.Errorf("gzip read: %w", err)
	}
	if err := wireFormSizeError(int64(len(out)), maximumBytes); err != nil {
		return "", err
	}
	return string(out), nil
}
