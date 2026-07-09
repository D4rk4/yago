package pageindex

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

func TestDocumentMetadataCarriesAuthor(t *testing.T) {
	values := documentMetadata(
		pageparse.ParsedPage{
			Description: "About",
			Author:      "Jane Doe",
			Keywords:    "go, search",
			Publisher:   "Example Press",
		},
		yagomodel.URIMetadataRow{Properties: map[string]string{yagomodel.URLMetaHash: "H"}},
	)
	if values["author"] != "Jane Doe" || values["description"] != "About" ||
		values["keywords"] != "go, search" || values["publisher"] != "Example Press" {
		t.Fatalf("metadata = %#v", values)
	}

	bare := documentMetadata(
		pageparse.ParsedPage{},
		yagomodel.URIMetadataRow{Properties: map[string]string{}},
	)
	for _, key := range []string{"author", "keywords", "publisher"} {
		if _, ok := bare[key]; ok {
			t.Fatalf("empty %s stored: %#v", key, bare)
		}
	}
}
