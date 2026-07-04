package rwi

import (
	"context"
	"maps"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestYaCyReferenceContainerRoundTripFixtures(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	urlHash, err := yagomodel.HashURL("http://test.org/test.html")
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	word := yagomodel.WordHash("test")
	stored := yagomodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yagomodel.ColURLHash:           urlHash.String(),
			yagomodel.ColTextPosition:      "5",
			yagomodel.ColWordDistance:      "0",
			yagomodel.ColHitCount:          "1",
			yagomodel.ColTextWordCount:     "20",
			yagomodel.ColPhraseCount:       "3",
			yagomodel.ColLocalLinkCount:    "1",
			yagomodel.ColExternalLinkCount: "1",
			yagomodel.ColLanguage:          "en",
		},
	}

	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{stored}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	var retrieved *yagomodel.RWIPosting
	err = h.rwi.Index.ScanWord(ctx, word, func(entry yagomodel.RWIPosting) (bool, error) {
		if entry.Properties[yagomodel.ColURLHash] == urlHash.String() {
			retrieved = &entry

			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if retrieved == nil {
		t.Fatal("stored posting not found by url hash")
	}
	if !maps.Equal(retrieved.Properties, stored.Properties) {
		t.Fatalf(
			"retrieved properties = %v, want stored %v",
			retrieved.Properties,
			stored.Properties,
		)
	}
}
