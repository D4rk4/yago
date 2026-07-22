package peerroster

import "testing"

func TestCurrentRosterEntryDecodeRejectsShortAndMalformedRecords(t *testing.T) {
	short := make([]byte, lastSeenWidth-1)
	if _, err := (rosterEntryCodec{}).Decode(short); err == nil {
		t.Fatal("current roster decoder accepted a short record")
	}

	malformedSeed := "not a seed"
	malformed := make([]byte, lastSeenWidth, lastSeenWidth+len(malformedSeed))
	malformed = append(malformed, malformedSeed...)
	if _, err := (rosterEntryCodec{}).Decode(malformed); err == nil {
		t.Fatal("current roster decoder accepted a malformed seed")
	}
}
