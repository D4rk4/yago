package rwi

import (
	"context"
	"maps"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

func TestYaCyReferenceContainerRoundTripFixtures(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	urlHash, err := yacymodel.HashURL("http://test.org/test.html")
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	word := yacymodel.WordHash("test")
	stored := yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:           urlHash.String(),
			yacymodel.ColTextPosition:      "5",
			yacymodel.ColWordDistance:      "0",
			yacymodel.ColHitCount:          "1",
			yacymodel.ColTextWordCount:     "20",
			yacymodel.ColPhraseCount:       "3",
			yacymodel.ColLocalLinkCount:    "1",
			yacymodel.ColExternalLinkCount: "1",
			yacymodel.ColLanguage:          "en",
		},
	}

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{stored}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	var retrieved *yacymodel.RWIPosting
	err = h.rwi.Index.ScanWord(ctx, word, func(entry yacymodel.RWIPosting) (bool, error) {
		if entry.Properties[yacymodel.ColURLHash] == urlHash.String() {
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
