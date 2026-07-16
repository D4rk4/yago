package querymatch

import "testing"

func TestTermCanMatchWithinToken(t *testing.T) {
	if !TermCanMatchWithinToken("東京") {
		t.Fatal("unsegmented-script term was not recognized")
	}
	if TermCanMatchWithinToken("api") {
		t.Fatal("whitespace-token term was treated as unsegmented")
	}
}
