package yagomodel_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestEncodeSeedWireFormUsesYaCyCompactVocabulary(t *testing.T) {
	t.Parallel()

	seed := yagomodel.Seed{Hash: yagomodel.WordHash("seed")}
	encoded := yagomodel.EncodeSeedWireForm(seed)
	if !strings.HasPrefix(encoded, "b|") && !strings.HasPrefix(encoded, "z|") {
		t.Fatalf("encoded seed = %q", encoded)
	}
	parsed, err := yagomodel.ParseSeedWireForm(t.Context(), encoded)
	if err != nil || parsed.Hash != seed.Hash {
		t.Fatalf("ParseSeedWireForm = %#v, %v", parsed, err)
	}

	compressible := yagomodel.Seed{
		Hash: yagomodel.WordHash("seed"),
		News: yagomodel.Some(strings.Repeat("a", 1024)),
	}
	compressed := yagomodel.EncodeSeedWireForm(compressible)
	if !strings.HasPrefix(compressed, "z|") {
		t.Fatalf("compressed seed = %q", compressed)
	}
	parsed, err = yagomodel.ParseSeedWireForm(t.Context(), compressed)
	if err != nil || parsed.Hash != compressible.Hash {
		t.Fatalf("ParseSeedWireForm compressed = %#v, %v", parsed, err)
	}
}
