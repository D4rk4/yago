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

func compressedWireForm(payload string) string {
	return tagged(wireFormGzip, Encode(gzipCompress(payload)))
}
