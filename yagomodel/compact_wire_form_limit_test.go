package yagomodel

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeWireFormInflatedBoundary(t *testing.T) {
	payload := strings.Repeat("x", int(maximumInflatedWireFormBytes))
	decoded, err := DecodeWireForm(t.Context(), compressedWireForm(payload))
	if err != nil {
		t.Fatalf("DecodeWireForm: %v", err)
	}
	if decoded != payload {
		t.Fatalf("decoded bytes = %d, want %d", len(decoded), len(payload))
	}
}

func TestDecodeWireFormRejectsInflatedBoundaryPlusOne(t *testing.T) {
	payload := strings.Repeat("x", int(maximumInflatedWireFormBytes)+1)
	if _, err := DecodeWireForm(
		t.Context(),
		compressedWireForm(payload),
	); !errors.Is(
		err,
		errInflatedWireFormTooLarge,
	) {
		t.Fatalf("DecodeWireForm error = %v", err)
	}
}

func TestDecodeWireFormRejectsSmallCompressedBomb(t *testing.T) {
	payload := strings.Repeat("x", int(maximumInflatedWireFormBytes)*2)
	encoded := compressedWireForm(payload)
	if len(encoded) >= 64<<10 {
		t.Fatalf("compressed bytes = %d, want less than 64 KiB", len(encoded))
	}
	if _, err := DecodeWireForm(
		t.Context(),
		encoded,
	); !errors.Is(
		err,
		errInflatedWireFormTooLarge,
	) {
		t.Fatalf("DecodeWireForm error = %v", err)
	}
}

func TestDecodeWireFormWithLimitBoundsPlainAndBase64Forms(t *testing.T) {
	forms := []string{
		"abc",
		tagged(wireFormPlain, "abc"),
		tagged(wireFormBase64, Encode([]byte("abc"))),
	}
	for _, form := range forms {
		decoded, err := DecodeWireFormWithLimit(t.Context(), form, 3)
		if err != nil || decoded != "abc" {
			t.Fatalf("exact limit for %q = %q, %v", form, decoded, err)
		}
		if _, err := DecodeWireFormWithLimit(
			t.Context(),
			form,
			2,
		); !errors.Is(err, errInflatedWireFormTooLarge) {
			t.Fatalf("over limit for %q = %v", form, err)
		}
	}
}

func TestDecodeWireFormWithLimitValidatesBase64BeforeAllocation(t *testing.T) {
	if _, err := DecodeWireFormWithLimit(
		t.Context(),
		"b|@@@",
		1,
	); !errors.Is(err, ErrInvalidBase64) {
		t.Fatalf("invalid base64 error = %v", err)
	}
	decoded, err := DecodeWireFormWithLimit(t.Context(), "b|YW\nJj", 3)
	if err != nil || decoded != "abc" {
		t.Fatalf("newline base64 = %q, %v", decoded, err)
	}
}

func TestDecodeWireFormWithLimitBoundsCompressedInputAndOutput(t *testing.T) {
	payload := strings.Repeat("x", 64)
	encoded := compressedWireForm(payload)
	decoded, err := DecodeWireFormWithLimit(t.Context(), encoded, int64(len(payload)))
	if err != nil || decoded != payload {
		t.Fatalf("exact compressed limit = %d bytes, %v", len(decoded), err)
	}
	if _, err := DecodeWireFormWithLimit(
		t.Context(),
		encoded,
		int64(len(gzipCompress(payload)))-1,
	); !errors.Is(err, errInflatedWireFormTooLarge) {
		t.Fatalf("compressed input limit error = %v", err)
	}
	if _, err := DecodeWireFormWithLimit(
		t.Context(),
		compressedWireForm(strings.Repeat("x", 33)),
		32,
	); !errors.Is(err, errInflatedWireFormTooLarge) {
		t.Fatalf("compressed output limit error = %v", err)
	}
}

func TestDecodeWireFormWithLimitAcceptsEmptyAtZero(t *testing.T) {
	for _, form := range []string{"", "p|", "b|"} {
		decoded, err := DecodeWireFormWithLimit(t.Context(), form, 0)
		if err != nil || decoded != "" {
			t.Fatalf("empty form %q = %q, %v", form, decoded, err)
		}
	}
}

func compressedWireForm(payload string) string {
	return tagged(wireFormGzip, Encode(gzipCompress(payload)))
}
