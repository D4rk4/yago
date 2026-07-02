package yacymodel_test

import (
	"testing"

	"github.com/D4rk4/yago/yacymodel"
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

func TestDecodeSearchIndexAbstract(t *testing.T) {
	t.Parallel()

	got, err := yacymodel.DecodeSearchIndexAbstract("{AAAAAA:bbbbbbcccccc,BBBBBB:aaaaaa}")
	if err != nil {
		t.Fatalf("DecodeSearchIndexAbstract: %v", err)
	}
	want := []yacymodel.Hash{"bbbbbbAAAAAA", "ccccccAAAAAA", "aaaaaaBBBBBB"}
	if len(got) != len(want) {
		t.Fatalf("hashes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hashes = %v, want %v", got, want)
		}
	}
}

func TestDecodeSearchIndexAbstractEmpty(t *testing.T) {
	t.Parallel()

	got, err := yacymodel.DecodeSearchIndexAbstract("{}")
	if err != nil {
		t.Fatalf("DecodeSearchIndexAbstract: %v", err)
	}
	if got != nil {
		t.Fatalf("hashes = %v, want nil", got)
	}
}

func TestDecodeSearchIndexAbstractRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"AAAAAA:bbbbbb",
		"{short:bbbbbb}",
		"{AAAAAA:short}",
		"{AAAAAAbbbbbb}",
		"{AAAAA@:bbbbbb}",
		"{AAAAAA:bbbbb@}",
	} {
		if _, err := yacymodel.DecodeSearchIndexAbstract(raw); err == nil {
			t.Fatalf("DecodeSearchIndexAbstract(%q) should fail", raw)
		}
	}
}
