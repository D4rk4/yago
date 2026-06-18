package yacymodel

import (
	"errors"
	"testing"
)

func TestDecodeBitfieldInvalid(t *testing.T) {
	if _, err := DecodeBitfield("AA=A"); !errors.Is(err, errInvalidBitfield) {
		t.Fatalf("DecodeBitfield invalid = %v, want errInvalidBitfield", err)
	}
}

func TestBitfieldGet(t *testing.T) {
	field := Bitfield([]byte{0b00000001, 0b00010000})
	cases := map[int]bool{
		0:  true,
		1:  false,
		8:  false,
		12: true,
		16: false,
		-1: false,
	}
	for pos, want := range cases {
		if got := field.Get(pos); got != want {
			t.Errorf("Get(%d) = %v, want %v", pos, got, want)
		}
	}
}

func TestBitfieldAllSet(t *testing.T) {
	full := Bitfield([]byte{0xff, 0xff, 0xff, 0xff})
	if !full.AllSet(32) {
		t.Fatal("AllSet(32) = false for all-ones, want true")
	}
	partial := Bitfield([]byte{0xff, 0xff, 0xff, 0xfe})
	if partial.AllSet(32) {
		t.Fatal("AllSet(32) = true for cleared bit, want false")
	}
}
