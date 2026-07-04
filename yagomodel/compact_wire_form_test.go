package yagomodel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCompactWireFormRoundTrip(t *testing.T) {
	for _, payload := range []string{
		sampleSeed,
		"",
		strings.Repeat("Key=Value,", 100),
	} {
		got, err := DecodeWireForm(context.Background(), EncodeCompactWireForm(payload))
		if err != nil {
			t.Fatalf("round trip %q: %v", payload, err)
		}
		if got != payload {
			t.Errorf("round trip:\n got %q\nwant %q", got, payload)
		}
	}
}

func TestEncodeCompactWireFormPicksShortest(t *testing.T) {
	short := EncodeCompactWireForm("Hash=ABCDEFGHIJKL")
	if short[0] != wireFormPlain {
		t.Errorf("short payload should stay plain, got tag %q", short[0])
	}
	long := EncodeCompactWireForm(strings.Repeat("Key=Value,", 200))
	if long[0] != wireFormGzip {
		t.Errorf("highly compressible payload should gzip, got tag %q", long[0])
	}
}

func TestEncodeBase64WireFormIsPropertySafe(t *testing.T) {
	raw := "http://example.com/p?a=b,c={x}&d=e"
	form := EncodeBase64WireForm(raw)
	if strings.ContainsAny(form[2:], ",={}") {
		t.Errorf("base64 wire form must avoid property delimiters: %q", form)
	}
	got, err := DecodeWireForm(context.Background(), form)
	if err != nil || got != raw {
		t.Errorf("round trip = %q, %v", got, err)
	}
}

func TestDecodeWireFormExplicit(t *testing.T) {
	plain, err := DecodeWireForm(context.Background(), "p|Hash=ABCDEFGHIJKL")
	if err != nil || plain != "Hash=ABCDEFGHIJKL" {
		t.Errorf("plain decode = %q, %v", plain, err)
	}
	b64, err := DecodeWireForm(context.Background(), "b|"+Encode([]byte("{Hash=ABCDEFGHIJKL}")))
	if err != nil || b64 != "{Hash=ABCDEFGHIJKL}" {
		t.Errorf("b64 decode = %q, %v", b64, err)
	}
}

func TestDecodeWireFormAcceptsBarePayload(t *testing.T) {
	for _, input := range []string{
		"{Hash=ABCDEFGHIJKL}",
		"Hash=ABCDEFGHIJKL",
		"",
		"x",
	} {
		got, err := DecodeWireForm(t.Context(), input)
		if err != nil {
			t.Fatalf("DecodeWireForm(%q) error = %v", input, err)
		}
		if got != input {
			t.Errorf("DecodeWireForm(%q) = %q", input, got)
		}
	}
}

func TestDecodeWireFormErrors(t *testing.T) {
	for _, bad := range []string{"q|data", "b|===", "z|" + Encode([]byte("not gzip"))} {
		if _, err := DecodeWireForm(t.Context(), bad); err == nil {
			t.Errorf("DecodeWireForm(%q) = nil error", bad)
		}
	}
	corrupt := gzipCompress("payload")
	corrupt[len(corrupt)-1] ^= 0xff
	if _, err := DecodeWireForm(t.Context(), "z|"+Encode(corrupt)); err == nil {
		t.Error("corrupt gzip payload should fail")
	}
	if _, err := DecodeWireForm(t.Context(), "q|data"); !errors.Is(err, ErrBadWireForm) {
		t.Errorf("unknown tag should be ErrBadWireForm")
	}
}
