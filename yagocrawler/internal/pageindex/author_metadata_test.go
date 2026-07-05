package pageindex

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

func TestDocumentMetadataCarriesAuthor(t *testing.T) {
	values := documentMetadata(
		pageparse.ParsedPage{Description: "About", Author: "Jane Doe"},
		yagomodel.URIMetadataRow{Properties: map[string]string{yagomodel.URLMetaHash: "H"}},
	)
	if values["author"] != "Jane Doe" || values["description"] != "About" {
		t.Fatalf("metadata = %#v", values)
	}

	bare := documentMetadata(
		pageparse.ParsedPage{},
		yagomodel.URIMetadataRow{Properties: map[string]string{}},
	)
	if _, ok := bare["author"]; ok {
		t.Fatalf("empty author stored: %#v", bare)
	}
}
