package rwi

import (
	"maps"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRecognizesPostingRequiresMatchingKeyAndStoredPosting(t *testing.T) {
	entry := posting("recognized-word", "recognized-url")
	url, err := entry.URLHash()
	if err != nil {
		t.Fatalf("posting URL hash: %v", err)
	}
	key := postingKey(entry.WordHash, url.Hash())
	raw := encodeStoredPosting(entry)
	if !RecognizesPosting(key, raw) {
		t.Fatal("valid stored posting was not recognized")
	}
	if RecognizesPosting(vault.Key("short"), raw) {
		t.Fatal("short posting key was recognized")
	}
	if RecognizesPosting(key, []byte{storedPostingFormatV1}) {
		t.Fatal("malformed stored posting was recognized")
	}

	wrong := entry
	wrong.Properties = maps.Clone(entry.Properties)
	wrong.Properties[yagomodel.ColURLHash] = yagomodel.WordHash("different-url").String()
	if RecognizesPosting(key, encodeStoredPosting(wrong)) {
		t.Fatal("posting with a different URL hash was recognized")
	}
}
