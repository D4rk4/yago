package yacymodel

import "testing"

func TestPosition(t *testing.T) {
	low, err := Position("AAAAAAAAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	high, err := Position("__________AA")
	if err != nil {
		t.Fatal(err)
	}
	if low >= high {
		t.Errorf("expected ring order low(%d) < high(%d)", low, high)
	}
	if high != MaxPosition {
		t.Errorf("Position of all-last folded symbols = %d, want %d", high, MaxPosition)
	}
}

func TestPositionInvalid(t *testing.T) {
	if _, err := Position("===========A"); err == nil {
		t.Fatal("expected error for invalid hash symbols")
	}
	if _, err := Position(""); err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestDistance(t *testing.T) {
	if d := Distance(10, 40); d != 30 {
		t.Errorf("Distance(10,40) = %d, want 30", d)
	}
	if d := Distance(40, 10); d != (MaxPosition-40)+10+1 {
		t.Errorf("Distance wrap = %d", d)
	}
	if d := Distance(5, 5); d != 0 {
		t.Errorf("Distance(5,5) = %d, want 0", d)
	}
}
