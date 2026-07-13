package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
)

func TestDocumentAuthorityCitationsHonorAnchorRelations(t *testing.T) {
	doc := documentstore.Document{
		CanonicalURL:                "https://source.example/page",
		OutboundAnchorEvidenceKnown: true,
		Outlinks:                    []string{"https://legacy.example/"},
		OutboundAnchors: []documentstore.OutboundAnchor{
			{TargetURL: "https://trusted.example/", Text: "trusted"},
			{TargetURL: "https://nofollow.example/", NoFollow: true},
			{TargetURL: "https://community.example/", UserGenerated: true},
			{TargetURL: "https://promotion.example/", Sponsored: true},
		},
	}
	citations := collectDocumentAuthorityCitations(doc)
	if len(citations) != 1 || citations[0].SourceURL != doc.CanonicalURL ||
		citations[0].TargetURL != "https://trusted.example/" ||
		citations[0].Confidence != 1 {
		t.Fatalf("citations = %#v", citations)
	}
	doc.OutboundAnchorEvidenceKnown = false
	citations = collectDocumentAuthorityCitations(doc)
	if len(citations) != 1 || citations[0].TargetURL != "https://legacy.example/" ||
		citations[0].Confidence != 0.4 {
		t.Fatalf("legacy citations = %#v", citations)
	}
	if citations := collectDocumentAuthorityCitations(documentstore.Document{}); citations != nil {
		t.Fatalf("empty document citations = %#v", citations)
	}
}

func collectDocumentAuthorityCitations(doc documentstore.Document) []hostrank.Citation {
	var citations []hostrank.Citation
	visitDocumentAuthorityCitations(doc, func(citation hostrank.Citation) {
		citations = append(citations, citation)
	})

	return citations
}
