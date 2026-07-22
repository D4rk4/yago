package seedlist

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
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
		{"0", 0.110, true},
		{"", 0.110, true},
		{" ", 0.110, true},
		{"123456789x", 0.110, true},
		{"124353432xxxx4546399999", 0.110, true},
	}
	for _, c := range cases {
		seed := yagomodel.Seed{
			Hash:    yagomodel.WordHash("versioned"),
			Version: yagomodel.Some(yagomodel.YaCyVersion(c.version)),
		}
		if got := seedPassesVersionFloor(seed, c.floor); got != c.keep {
			t.Errorf(
				"seedPassesVersionFloor(%q, %v) = %v, want %v",
				c.version,
				c.floor,
				got,
				c.keep,
			)
		}
	}
}

func TestYaCyMinVersionKeepsDeveloperSeedWithoutVersion(t *testing.T) {
	seed := yagomodel.Seed{Hash: yagomodel.WordHash("unversioned")}
	if !seedPassesVersionFloor(seed, 0.110) {
		t.Error("seed without a version must pass a minversion filter")
	}
}

func TestYaCyMinVersionParsesStoredVersionWithJavaDoubleRules(t *testing.T) {
	if seedPassesVersionFloor(
		yagomodel.Seed{Version: yagomodel.Some(yagomodel.YaCyVersion("\x1f 0.5d \x00"))},
		1,
	) {
		t.Fatal("below-floor Java double literal passed")
	}
	if !seedPassesVersionFloor(
		yagomodel.Seed{Version: yagomodel.Some(yagomodel.YaCyVersion("0_5"))},
		1,
	) {
		t.Fatal("invalid Java double literal did not retain developer eligibility")
	}
}
