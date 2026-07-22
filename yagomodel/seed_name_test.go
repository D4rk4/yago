package yagomodel_test

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestNormalizeSeedNameMatchesYaCyLookupVocabulary(t *testing.T) {
	t.Parallel()

	if got := yagomodel.NormalizeSeedName(" <Peer> "); got != " _peer_ " {
		t.Fatalf("NormalizeSeedName = %q", got)
	}
}
