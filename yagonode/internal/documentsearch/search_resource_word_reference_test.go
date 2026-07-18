package documentsearch

import (
	"context"
	"maps"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestSearchAttachesSelectedPostingWithoutMutatingStoredMetadata(t *testing.T) {
	word := hashFor("word")
	posting := postingEntry(word, "url", 7, 3)
	stored := urlRows("url")
	search := searcher{
		index: fakeScanner{
			postings: map[yagomodel.Hash][]yagomodel.RWIPosting{word: {posting}},
		},
		documents:      fakeDirectory{rows: stored},
		matchesPerTerm: 10,
	}

	result, err := search.search(
		context.Background(),
		searchCriteria{terms: []yagomodel.Hash{word}, maxResults: 1},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(result.resources))
	}
	encoded := result.resources[0].Properties[searchResourceWordReference]
	decoded, err := yagomodel.Decode(encoded)
	if err != nil {
		t.Fatalf("decode wi: %v", err)
	}
	if got, want := string(
		decoded,
	), "{h=urlAAAAAAAAA,a=0,s=0,u=0,w=0,p=0,d=0,l=en,x=0,y=0,m=0,n=0,g=0,z=AAAAAA,c=3,t=7,r=0,o=0,i=0,k=0}"; got != want {
		t.Fatalf("decoded wi = %q, want %q", got, want)
	}
	if _, persisted := stored[hashFor("url")].Properties[searchResourceWordReference]; persisted {
		t.Fatal("stored metadata contains transient wi")
	}
}

func TestResourcesWithoutSelectedPostingRemainUnchanged(t *testing.T) {
	stored := urlRows("url")[hashFor("url")]

	resources := resourcesWithWordReferences(
		[]yagomodel.URIMetadataRow{stored},
		map[yagomodel.Hash]matchedDocument{},
	)
	if len(resources) != 1 || !maps.Equal(resources[0].Properties, stored.Properties) {
		t.Fatalf("resources = %+v, want unchanged row", resources)
	}
}
