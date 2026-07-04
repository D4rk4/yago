package yagomodel

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHash(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"ABCDEFGHIJKL", true},
		{"____________", true},
		{"short", false},
		{"ABCDEFGHIJKLM", false},
		{"ABCDEFGHIJK=", false},
	}
	for _, c := range cases {
		_, err := ParseHash(c.in)
		if (err == nil) != c.ok {
			t.Errorf("ParseHash(%q) err = %v, want ok=%v", c.in, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrInvalidHash) {
			t.Errorf("ParseHash(%q) = %v, want ErrInvalidHash", c.in, err)
		}
	}
}

func TestWordHash(t *testing.T) {
	h := WordHash("Hello")
	if !h.Valid() {
		t.Fatalf("WordHash produced invalid hash %q", h)
	}
	if len(h) != HashLength {
		t.Errorf("WordHash length = %d, want %d", len(h), HashLength)
	}
	if h != WordHash("hello") {
		t.Errorf("WordHash must lower-case: %q != %q", h, WordHash("hello"))
	}
	if WordHash("hello") == WordHash("world") {
		t.Error("distinct words must hash distinctly")
	}
}

func TestAvoidReservedWordHash(t *testing.T) {
	high := string(Alphabet[len(Alphabet)-1])
	low := string(Alphabet[0])
	h := []byte(strings.Repeat(high, 5) + "BCDEFG0")
	avoidReservedWordHash(h)
	if string(h) != strings.Repeat(high, 4)+"BCDEFG0"+low {
		t.Fatalf("reserved word hash correction = %q", h)
	}
}

func TestHashReserved(t *testing.T) {
	if !Hash("_____ABCDEFG").Reserved() {
		t.Error("expected reserved prefix to be detected")
	}
	if Hash("ABCDEFGHIJKL").Reserved() {
		t.Error("unexpected reserved detection")
	}
}
