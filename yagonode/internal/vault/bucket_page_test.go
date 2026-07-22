package vault

import (
	"errors"
	"testing"
)

type pageReaderBucket struct {
	*scriptedBucket
	page  BucketPage
	err   error
	after Key
	limit int
}

type keyPageReaderBucket struct {
	*scriptedBucket
	page  BucketKeyPage
	err   error
	after Key
	limit int
}

func (b *keyPageReaderBucket) ReadKeyPageAfter(after Key, limit int) (BucketKeyPage, error) {
	b.after = append(Key(nil), after...)
	b.limit = limit

	return b.page, b.err
}

func (b *pageReaderBucket) ReadPageAfter(after Key, limit int) (BucketPage, error) {
	b.after = append(Key(nil), after...)
	b.limit = limit

	return b.page, b.err
}

func TestReadBucketPageCopiesEntriesAndForwardsCursor(t *testing.T) {
	key := Key("b")
	value := []byte("value")
	bucket := &pageReaderBucket{
		scriptedBucket: &scriptedBucket{},
		page: BucketPage{
			Entries: []BucketPageEntry{{Key: key, Value: value}},
			More:    true,
		},
	}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	page, err := tx.ReadBucketPage("documents", Key("a"), 2)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'x'
	value[0] = 'X'
	if got := string(page.Entries[0].Key); got != "b" {
		t.Fatalf("copied key = %q", got)
	}
	if got := string(page.Entries[0].Value); got != "value" {
		t.Fatalf("copied value = %q", got)
	}
	if string(bucket.after) != "a" || bucket.limit != 2 || !page.More {
		t.Fatalf("cursor/limit/more = %q/%d/%t", bucket.after, bucket.limit, page.More)
	}
}

func TestReadBucketPageRejectsInvalidAndUnavailableReaders(t *testing.T) {
	tx := &Txn{etx: presenceTxn{bucket: &scriptedBucket{}}}
	if _, err := tx.ReadBucketPage("documents", nil, 0); err == nil {
		t.Fatal("zero page limit accepted")
	}
	_, err := tx.ReadBucketPage("documents", nil, 1)
	if !errors.Is(err, errBucketPageUnavailable) {
		t.Fatalf("unavailable reader error = %v", err)
	}
}

func TestReadBucketPageWrapsReaderError(t *testing.T) {
	sentinel := errors.New("page failed")
	bucket := &pageReaderBucket{scriptedBucket: &scriptedBucket{}, err: sentinel}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	if _, err := tx.ReadBucketPage("documents", nil, 1); !errors.Is(err, sentinel) {
		t.Fatalf("reader error = %v, want %v", err, sentinel)
	}
}

func TestReadBucketPageRejectsMalformedPages(t *testing.T) {
	tests := []struct {
		name  string
		after Key
		limit int
		page  BucketPage
	}{
		{name: "empty continuation", limit: 1, page: BucketPage{More: true}},
		{
			name:  "oversized",
			limit: 1,
			page: BucketPage{Entries: []BucketPageEntry{
				{Key: Key("a")}, {Key: Key("b")},
			}},
		},
		{
			name:  "cursor replay",
			after: Key("b"),
			limit: 1,
			page:  BucketPage{Entries: []BucketPageEntry{{Key: Key("b")}}},
		},
		{
			name:  "unordered",
			limit: 2,
			page: BucketPage{Entries: []BucketPageEntry{
				{Key: Key("b")}, {Key: Key("a")},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bucket := &pageReaderBucket{scriptedBucket: &scriptedBucket{}, page: test.page}
			tx := &Txn{etx: presenceTxn{bucket: bucket}}
			_, err := tx.ReadBucketPage("documents", test.after, test.limit)
			if !errors.Is(err, errInvalidBucketPage) {
				t.Fatalf("malformed page error = %v", err)
			}
		})
	}
}

func TestReadBucketKeyPageCopiesKeysAndForwardsCursor(t *testing.T) {
	key := Key("b")
	bucket := &keyPageReaderBucket{
		scriptedBucket: &scriptedBucket{},
		page:           BucketKeyPage{Keys: []Key{key}, More: true},
	}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	page, err := tx.ReadBucketKeyPage("documents", Key("a"), 2)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'x'
	if got := string(page.Keys[0]); got != "b" {
		t.Fatalf("copied key = %q", got)
	}
	if string(bucket.after) != "a" || bucket.limit != 2 || !page.More {
		t.Fatalf("cursor/limit/more = %q/%d/%t", bucket.after, bucket.limit, page.More)
	}
}

func TestReadBucketKeyPageFallsBackToBoundedScan(t *testing.T) {
	bucket := &scriptedBucket{values: map[string][]byte{
		"a": []byte("first"),
		"b": []byte("second"),
		"c": []byte("third"),
	}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	page, err := tx.ReadBucketKeyPage("documents", Key("a"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Keys) != 1 || string(page.Keys[0]) != "b" || !page.More {
		t.Fatalf("page = %#v", page)
	}
}

func TestReadBucketKeyPageWrapsFallbackScanError(t *testing.T) {
	sentinel := errors.New("scan failed")
	bucket := &scriptedBucket{values: map[string][]byte{}, scanErr: sentinel}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	if _, err := tx.ReadBucketKeyPage("documents", nil, 1); !errors.Is(err, sentinel) {
		t.Fatalf("fallback scan error = %v, want %v", err, sentinel)
	}
}

func TestReadBucketKeyPageRejectsInvalidInputAndReaderOutput(t *testing.T) {
	tx := &Txn{etx: presenceTxn{bucket: &scriptedBucket{}}}
	if _, err := tx.ReadBucketKeyPage("documents", nil, 0); err == nil {
		t.Fatal("zero key page limit accepted")
	}
	sentinel := errors.New("key page failed")
	reader := &keyPageReaderBucket{scriptedBucket: &scriptedBucket{}, err: sentinel}
	if _, err := (&Txn{etx: presenceTxn{bucket: reader}}).ReadBucketKeyPage(
		"documents", nil, 1,
	); !errors.Is(err, sentinel) {
		t.Fatalf("reader error = %v, want %v", err, sentinel)
	}
	for _, page := range []BucketKeyPage{
		{More: true},
		{Keys: []Key{Key("a"), Key("b")}},
		{Keys: []Key{Key("b"), Key("a")}},
	} {
		reader := &keyPageReaderBucket{scriptedBucket: &scriptedBucket{}, page: page}
		if _, err := (&Txn{etx: presenceTxn{bucket: reader}}).ReadBucketKeyPage(
			"documents", nil, 1,
		); !errors.Is(err, errInvalidBucketPage) {
			t.Fatalf("malformed key page error = %v", err)
		}
	}
	replayed := &keyPageReaderBucket{
		scriptedBucket: &scriptedBucket{}, page: BucketKeyPage{Keys: []Key{Key("a")}},
	}
	if _, err := (&Txn{etx: presenceTxn{bucket: replayed}}).ReadBucketKeyPage(
		"documents", Key("a"), 1,
	); !errors.Is(err, errInvalidBucketPage) {
		t.Fatalf("replayed key page error = %v", err)
	}
}
