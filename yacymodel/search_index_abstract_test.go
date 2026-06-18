package yacymodel_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestEncodeSearchIndexAbstractGroupsURLHashes(t *testing.T) {
	t.Parallel()

	got := yacymodel.EncodeSearchIndexAbstract([]yacymodel.Hash{
		"bbbbbbAAAAAA",
		"aaaaaaBBBBBB",
		"ccccccAAAAAA",
	})
	want := "{AAAAAA:bbbbbbcccccc,BBBBBB:aaaaaa}"
	if got != want {
		t.Fatalf("abstract = %q, want %q", got, want)
	}
}

func TestEncodeSearchIndexAbstractEmpty(t *testing.T) {
	t.Parallel()

	if got := yacymodel.EncodeSearchIndexAbstract(nil); got != "{}" {
		t.Fatalf("abstract = %q, want {}", got)
	}
}
