package formatparse

import "testing"

// TestDecodeCP1252 covers the three ranges: ASCII identity, the Windows-1252
// high block, an undefined slot decoded as its raw value, and the Latin-1 tail.
func TestDecodeCP1252(t *testing.T) {
	cases := map[byte]rune{
		'A':  'A',  // ASCII identity.
		0x92: '’',  // right single quotation mark.
		0x80: '€',  // euro sign.
		0x81: 0x81, // undefined slot -> raw value.
		0xE9: 0xE9, // Latin-1 é identity.
	}
	for in, want := range cases {
		if got := decodeCP1252(in); got != want {
			t.Fatalf("decodeCP1252(%#x) = %#x, want %#x", in, got, want)
		}
	}
}
