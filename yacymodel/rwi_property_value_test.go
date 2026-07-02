package yacymodel

import "testing"

func TestRWIPostingCardinal(t *testing.T) {
	posting := RWIPosting{Properties: map[string]string{"count": "42"}}
	got, err := posting.Cardinal("count")
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Fatalf("Cardinal = %d", got)
	}
}

func TestRWIPostingCardinalRejectsBadValue(t *testing.T) {
	posting := RWIPosting{Properties: map[string]string{"count": "not-a-number"}}
	if _, err := posting.Cardinal("count"); err == nil {
		t.Fatal("bad cardinal should fail")
	}
}

func TestRWIPostingByteValueRejectsOverflow(t *testing.T) {
	posting := RWIPosting{Properties: map[string]string{"value": "256"}}
	if _, err := posting.ByteValue("value"); err == nil {
		t.Fatal("overflow byte value should fail")
	}
}
