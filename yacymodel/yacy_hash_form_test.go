package yacymodel

import "testing"

func TestYaCyHashHex(t *testing.T) {
	if got := YaCyHashHex("hello"); got != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("YaCyHashHex = %q", got)
	}
}
