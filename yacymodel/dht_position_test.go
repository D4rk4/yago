package yacymodel

import (
	"errors"
	"testing"
)

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

func TestVerticalPartition(t *testing.T) {
	low, err := VerticalPartition("AAAAAAAAAAAA", 2)
	if err != nil {
		t.Fatal(err)
	}
	if low != 0 {
		t.Fatalf("VerticalPartition low = %d, want 0", low)
	}

	high, err := VerticalPartition("__________AA", 2)
	if err != nil {
		t.Fatal(err)
	}
	if high != 3 {
		t.Fatalf("VerticalPartition high = %d, want 3", high)
	}

	only, err := VerticalPartition("__________AA", 0)
	if err != nil {
		t.Fatal(err)
	}
	if only != 0 {
		t.Fatalf("VerticalPartition exponent 0 = %d, want 0", only)
	}
}

func TestVerticalPosition(t *testing.T) {
	word := Hash("AAAAAAAAAAAA")
	position, err := VerticalPosition(word, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := position >> 61; got != 2 {
		t.Fatalf("VerticalPosition partition = %d, want 2", got)
	}

	base, err := Position(word)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := VerticalPosition(word, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if zero != base {
		t.Fatalf("VerticalPosition exponent 0 = %d, want %d", zero, base)
	}
}

func TestVerticalPartitionRejectsInvalidInput(t *testing.T) {
	if _, err := VerticalPartition("bad", 0); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("VerticalPartition bad hash = %v, want ErrInvalidHash", err)
	}
	if _, err := VerticalPartition("AAAAAAAAAAAA", -1); !errors.Is(err, ErrInvalidDHTPartition) {
		t.Fatalf("VerticalPartition bad exponent = %v, want ErrInvalidDHTPartition", err)
	}
	if _, err := VerticalPartition("AAAAAAAAAAAA", 63); !errors.Is(err, ErrInvalidDHTPartition) {
		t.Fatalf("VerticalPartition high exponent = %v, want ErrInvalidDHTPartition", err)
	}
}

func TestVerticalPositionRejectsInvalidInput(t *testing.T) {
	if _, err := VerticalPosition("bad", 0, 0); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("VerticalPosition bad hash = %v, want ErrInvalidHash", err)
	}
	if _, err := VerticalPosition("AAAAAAAAAAAA", 0, -1); !errors.Is(
		err,
		ErrInvalidDHTPartition,
	) {
		t.Fatalf("VerticalPosition bad exponent = %v, want ErrInvalidDHTPartition", err)
	}
	if _, err := VerticalPosition("AAAAAAAAAAAA", 4, 2); !errors.Is(
		err,
		ErrInvalidDHTPartition,
	) {
		t.Fatalf("VerticalPosition bad vertical = %v, want ErrInvalidDHTPartition", err)
	}
}
