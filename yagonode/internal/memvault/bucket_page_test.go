package memvault

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestMemBucketReadsExclusiveOrderedPages(t *testing.T) {
	bucket := memBucket{entries: map[string][]byte{
		"e": []byte("value-e"),
		"a": []byte("value-a"),
		"c": []byte("value-c"),
	}}
	assertMemPage(t, bucket, nil, "a,c", true)
	assertMemPage(t, bucket, vault.Key("c"), "e", false)
	assertMemPage(t, bucket, vault.Key("b"), "c,e", false)
	assertMemPage(t, bucket, vault.Key("z"), "", false)
}

func assertMemPage(
	t *testing.T,
	bucket memBucket,
	after vault.Key,
	want string,
	more bool,
) {
	t.Helper()
	page, err := bucket.ReadPageAfter(after, 2)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]byte, 0, len(page.Entries)*2)
	for _, entry := range page.Entries {
		if len(keys) > 0 {
			keys = append(keys, ',')
		}
		keys = append(keys, entry.Key...)
	}
	if string(keys) != want || page.More != more {
		t.Fatalf("page after %q = %q/%t, want %q/%t", after, keys, page.More, want, more)
	}
}
