package yagomodel

import "testing"

func TestEncodeSearchIndexAbstractSkipsInvalidHashes(t *testing.T) {
	if got := EncodeSearchIndexAbstract([]Hash{"short"}); got != "{}" {
		t.Fatalf("invalid-only abstract = %q", got)
	}
}

func TestCompareBase64Strings(t *testing.T) {
	if got := compareBase64Strings("A", "B"); got >= 0 {
		t.Fatalf("A before B compare = %d", got)
	}
	if got := compareBase64Strings("B", "A"); got <= 0 {
		t.Fatalf("B after A compare = %d", got)
	}
	if got := compareBase64Strings("A", "AA"); got >= 0 {
		t.Fatalf("shorter prefix compare = %d", got)
	}
	if got := compareBase64Strings("AA", "A"); got <= 0 {
		t.Fatalf("longer prefix compare = %d", got)
	}
	if got := compareBase64Strings("@", "A"); got <= 0 {
		t.Fatalf("non alphabet compare = %d", got)
	}
	if got := compareBase64Strings("same", "same"); got != 0 {
		t.Fatalf("same compare = %d", got)
	}
}
