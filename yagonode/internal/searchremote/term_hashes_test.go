package searchremote

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestTermHashesPreserveEveryNonblankWireTerm(t *testing.T) {
	hashes := termHashes([]string{"internet", "in", "montenegro"})
	if len(hashes) != 3 ||
		hashes[0] != yagomodel.WordHash("internet") ||
		hashes[1] != yagomodel.WordHash("in") ||
		hashes[2] != yagomodel.WordHash("montenegro") {
		t.Fatalf("wire hashes = %#v", hashes)
	}
	if hashes := termHashes([]string{" ", "query"}); len(hashes) != 1 ||
		hashes[0] != yagomodel.WordHash("query") {
		t.Fatalf("blank filtering = %#v", hashes)
	}
}
