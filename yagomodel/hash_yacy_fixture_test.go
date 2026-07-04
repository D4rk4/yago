package yagomodel

import "testing"

func TestYaCyQueryGoalWordHashFixtures(t *testing.T) {
	pairs := [][2]string{
		{"O'Reily's", "o'reily's"},
		{"Book", "book"},
		{"McGee", "mcgee"},
		{"Umphrey's", "umphrey's"},
		{"LIBRARY", "library"},
	}
	for _, pair := range pairs {
		if WordHash(pair[0]) != WordHash(pair[1]) {
			t.Errorf(
				"WordHash(%q) = %q, want %q from %q",
				pair[0],
				WordHash(pair[0]),
				WordHash(pair[1]),
				pair[1],
			)
		}
	}

	if WordHash("o'reily's") == WordHash("oreilys") {
		t.Error("apostrophes must stay part of the hashed word")
	}
}
