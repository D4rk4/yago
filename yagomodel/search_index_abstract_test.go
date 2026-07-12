package yagomodel_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestEncodeSearchIndexAbstractGroupsURLHashes(t *testing.T) {
	t.Parallel()

	got := yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{
		"bbbbbbAAAAAA",
		"aaaaaaBBBBBB",
		"ccccccAAAAAA",
	})
	want := "{AAAAAA:bbbbbbcccccc,BBBBBB:aaaaaa}"
	if got != want {
		t.Fatalf("abstract = %q, want %q", got, want)
	}
}

func TestDecodeSearchIndexAbstractRetainsOnlyRequestedHashes(t *testing.T) {
	t.Parallel()
	raw := "{AAAAAA:" + strings.Repeat("bbbbbb", 40_000) + "}"
	got, err := yagomodel.DecodeSearchIndexAbstractWithLimit(raw, 3)
	if err != nil {
		t.Fatalf("DecodeSearchIndexAbstractWithLimit: %v", err)
	}
	if len(got) != 3 || cap(got) != 3 {
		t.Fatalf("bounded hashes len=%d cap=%d", len(got), cap(got))
	}
	zero, err := yagomodel.DecodeSearchIndexAbstractWithLimit(raw, -1)
	if err != nil || zero != nil {
		t.Fatalf("zero limit = %v, %v", zero, err)
	}
}

func TestDecodeSearchIndexAbstractValidatesBeyondRetentionLimit(t *testing.T) {
	t.Parallel()
	raw := "{AAAAAA:bbbbbb" + strings.Repeat("cccccc", 100) + "ccccc@}"
	if _, err := yagomodel.DecodeSearchIndexAbstractWithLimit(raw, 1); err == nil {
		t.Fatal("malformed tail passed bounded decoding")
	}
	if _, err := yagomodel.DecodeSearchIndexAbstractWithLimit(
		"{AAAAAA:bbbbbb,}",
		1,
	); err == nil {
		t.Fatal("trailing group passed bounded decoding")
	}
}

func TestEncodeSearchIndexAbstractEmpty(t *testing.T) {
	t.Parallel()

	if got := yagomodel.EncodeSearchIndexAbstract(nil); got != "{}" {
		t.Fatalf("abstract = %q, want {}", got)
	}
}

func TestDecodeSearchIndexAbstract(t *testing.T) {
	t.Parallel()

	got, err := yagomodel.DecodeSearchIndexAbstract("{AAAAAA:bbbbbbcccccc,BBBBBB:aaaaaa}")
	if err != nil {
		t.Fatalf("DecodeSearchIndexAbstract: %v", err)
	}
	want := []yagomodel.Hash{"bbbbbbAAAAAA", "ccccccAAAAAA", "aaaaaaBBBBBB"}
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

	got, err := yagomodel.DecodeSearchIndexAbstract("{}")
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
		if _, err := yagomodel.DecodeSearchIndexAbstract(raw); err == nil {
			t.Fatalf("DecodeSearchIndexAbstract(%q) should fail", raw)
		}
	}
}
