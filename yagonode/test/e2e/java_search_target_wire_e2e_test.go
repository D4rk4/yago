//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestParseNodeSelfSeedAcceptsSeedlistWireForm(t *testing.T) {
	want := yagomodel.Seed{
		Hash: yagomodel.WordHash("self-seed"),
		Name: yagomodel.Some(strings.Repeat("self-seed-", 20)),
	}
	encoded := yagomodel.EncodeSeedWireForm(want)
	if !strings.HasPrefix(encoded, "z|") {
		t.Fatalf("wire form = %q, want compressed seed", encoded)
	}

	got, err := parseNodeSelfSeed(
		t.Context(),
		encoded+"\r\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash != want.Hash {
		t.Fatalf("hash = %q, want %q", got.Hash, want.Hash)
	}
}
