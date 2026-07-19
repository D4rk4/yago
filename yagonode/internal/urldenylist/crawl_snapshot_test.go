package urldenylist_test

import (
	"context"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

func TestSnapshotValuesAreSortedCopies(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	for _, entry := range []struct {
		kind  urldenylist.Kind
		value string
	}{
		{urldenylist.KindURL, "https://b.example/"},
		{urldenylist.KindURL, "https://a.example/"},
		{urldenylist.KindDomain, "z.example"},
		{urldenylist.KindDomain, "a.example"},
	} {
		if err := store.Add(ctx, entry.kind, entry.value); err != nil {
			t.Fatalf("Add(%q): %v", entry.value, err)
		}
	}
	urls, domains := store.Snapshot().Values()
	if !slices.Equal(urls, []string{"https://a.example/", "https://b.example/"}) {
		t.Fatalf("urls = %v", urls)
	}
	if !slices.Equal(domains, []string{"a.example", "z.example"}) {
		t.Fatalf("domains = %v", domains)
	}
	urls[0] = "mutated"
	domains[0] = "mutated"
	urls, domains = store.Snapshot().Values()
	if urls[0] != "https://a.example/" || domains[0] != "a.example" {
		t.Fatalf("snapshot values were mutated: %v %v", urls, domains)
	}
}
