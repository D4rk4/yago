package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestAnchorTextsKeepOnlyTrustedNonEmptyEvidence(t *testing.T) {
	got := anchorTexts([]documentstore.AnchorText{
		{Text: "trusted"},
		{Text: "nofollow", NoFollow: true},
		{Text: "community", UserGenerated: true},
		{Text: "promotion", Sponsored: true},
		{},
	})
	if len(got) != 1 || got[0] != "trusted" {
		t.Fatalf("anchor texts = %#v", got)
	}
}
