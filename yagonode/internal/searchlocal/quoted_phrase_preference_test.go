package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestPublicQuotedPhraseChangesLocalRanking(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, doc := range []documentstore.Document{
		{
			CanonicalURL: "https://a.example/",
			Title:        "Guide", ExtractedText: "brown fox quick lazy", Language: "en",
		},
		{
			CanonicalURL: "https://b.example/",
			Title:        "Guide", ExtractedText: "quick brown fox lazy", Language: "en",
		},
	} {
		if err := index.Index(t.Context(), doc); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}
	searcher := NewSearcher(index)
	unquoted, err := searchcore.ParsePublicRequest(searchcore.Request{
		Query: "quick brown", Source: searchcore.SourceLocal, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ParsePublicRequest unquoted: %v", err)
	}
	quoted, err := searchcore.ParsePublicRequest(searchcore.Request{
		Query: `"quick brown"`, Source: searchcore.SourceLocal, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ParsePublicRequest quoted: %v", err)
	}
	unquotedResponse, err := searcher.Search(t.Context(), unquoted)
	if err != nil {
		t.Fatalf("unquoted Search: %v", err)
	}
	quotedResponse, err := searcher.Search(t.Context(), quoted)
	if err != nil {
		t.Fatalf("quoted Search: %v", err)
	}
	if len(unquotedResponse.Results) != 2 || len(quotedResponse.Results) != 2 {
		t.Fatalf(
			"unquoted=%#v quoted=%#v",
			unquotedResponse.Results,
			quotedResponse.Results,
		)
	}
	if unquotedResponse.Results[0].URL != "https://a.example/" ||
		quotedResponse.Results[0].URL != "https://b.example/" {
		t.Fatalf(
			"unquoted order=%q quoted order=%q",
			[]string{unquotedResponse.Results[0].URL, unquotedResponse.Results[1].URL},
			[]string{quotedResponse.Results[0].URL, quotedResponse.Results[1].URL},
		)
	}
}
