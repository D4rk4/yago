package infrastructure

import (
	"testing"
)

func TestRWIPostingKeyRoundTrip(t *testing.T) {
	word := hashForStorageTest("word")
	url := hashForStorageTest("url")
	key := rwiPostingKey(word, url)

	if len(key) != rwiPostingKeyLength {
		t.Fatalf("key length = %d, want %d", len(key), rwiPostingKeyLength)
	}
	id, err := parseRWIPostingKey(key)
	if err != nil {
		t.Fatalf("parseRWIPostingKey: %v", err)
	}
	if id.WordHash != word || id.URLHash != url {
		t.Fatalf("id = %+v, want word=%s url=%s", id, word, url)
	}
}

func TestParseRWIPostingKeyRejectsMalformedLengths(t *testing.T) {
	valid := rwiPostingKey(hashForStorageTest("word"), hashForStorageTest("url"))
	cases := [][]byte{valid[:len(valid)-1], append(append([]byte(nil), valid...), 'x')}

	for _, key := range cases {
		if _, err := parseRWIPostingKey(key); err == nil {
			t.Fatalf("parseRWIPostingKey(%q) succeeded", key)
		}
	}
}
