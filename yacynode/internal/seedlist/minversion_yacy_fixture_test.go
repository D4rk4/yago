package seedlist

import (
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

func TestYaCyCombinedVersionMinVersionFixtures(t *testing.T) {
	cases := []struct {
		version string
		floor   float64
		keep    bool
	}{
		{"0.1100244", 0.110, true},
		{"0.1110244", 0.111, true},
		{"0.1090244", 0.110, false},
		{"0.111", 0.111, true},
		{"999.999999", 999.0, true},
		{"", 0.110, false},
		{" ", 0.110, false},
		{"123456789x", 0.110, false},
		{"124353432xxxx4546399999", 0.110, false},
	}
	for _, c := range cases {
		seed := yacymodel.Seed{
			Hash:    yacymodel.WordHash("versioned"),
			Version: yacymodel.Some(yacymodel.YaCyVersion(c.version)),
		}
		if got := seedVersionAtLeast(seed, c.floor); got != c.keep {
			t.Errorf(
				"seedVersionAtLeast(%q, %v) = %v, want %v",
				c.version,
				c.floor,
				got,
				c.keep,
			)
		}
	}
}

func TestYaCyMinVersionExcludesSeedWithoutVersion(t *testing.T) {
	seed := yacymodel.Seed{Hash: yacymodel.WordHash("unversioned")}
	if seedVersionAtLeast(seed, 0.110) {
		t.Error("seed without a version must not pass a minversion filter")
	}
}
