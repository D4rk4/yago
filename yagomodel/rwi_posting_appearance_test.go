package yagomodel

import (
	"strconv"
	"testing"
)

func TestRWIPostingDocType(t *testing.T) {
	entry := RWIPosting{
		Properties: map[string]string{ColDocType: strconv.FormatUint(uint64(DocTypeImage), 10)},
	}
	got, ok := entry.DocType()
	if !ok || got != DocTypeImage {
		t.Fatalf("DocType() = %q, %v, want %q, true", got, ok, DocTypeImage)
	}
}

func TestRWIPostingDocTypeMissing(t *testing.T) {
	entry := RWIPosting{Properties: map[string]string{}}
	if _, ok := entry.DocType(); ok {
		t.Fatal("DocType() ok = true for missing column, want false")
	}
}

func TestRWIPostingAppearanceFlags(t *testing.T) {
	flags := []byte{0, 0, 0, 0}
	flags[RWIFlagHasVideo>>3] |= 1 << (RWIFlagHasVideo % 8)
	entry := RWIPosting{Properties: map[string]string{ColFlags: Encode(flags)}}
	got, err := entry.AppearanceFlags()
	if err != nil {
		t.Fatalf("AppearanceFlags() error = %v", err)
	}
	if !got.Get(RWIFlagHasVideo) {
		t.Fatal("video flag = false, want true")
	}
	if got.Get(RWIFlagHasImage) {
		t.Fatal("image flag = true, want false")
	}
}

func TestRWIPostingAppearanceFlagsMissing(t *testing.T) {
	entry := RWIPosting{Properties: map[string]string{}}
	got, err := entry.AppearanceFlags()
	if err != nil || got != nil {
		t.Fatalf("AppearanceFlags() = %v, %v, want nil, nil", got, err)
	}
}
