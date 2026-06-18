package yacymodel

import "testing"

func TestRWIEntryDocType(t *testing.T) {
	entry := RWIEntry{Properties: map[string]string{ColDocType: Encode([]byte{DocTypeImage})}}
	got, ok := entry.DocType()
	if !ok || got != DocTypeImage {
		t.Fatalf("DocType() = %q, %v, want %q, true", got, ok, DocTypeImage)
	}
}

func TestRWIEntryDocTypeMissing(t *testing.T) {
	entry := RWIEntry{Properties: map[string]string{}}
	if _, ok := entry.DocType(); ok {
		t.Fatal("DocType() ok = true for missing column, want false")
	}
}

func TestRWIEntryAppearanceFlags(t *testing.T) {
	flags := []byte{0, 0, 0, 0}
	flags[RWIFlagHasVideo>>3] |= 1 << (RWIFlagHasVideo % 8)
	entry := RWIEntry{Properties: map[string]string{ColFlags: Encode(flags)}}
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

func TestRWIEntryAppearanceFlagsMissing(t *testing.T) {
	entry := RWIEntry{Properties: map[string]string{}}
	got, err := entry.AppearanceFlags()
	if err != nil || got != nil {
		t.Fatalf("AppearanceFlags() = %v, %v, want nil, nil", got, err)
	}
}
