package yacymodel

import (
	"errors"
	"fmt"
)

const Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var ErrInvalidBase64 = errors.New("invalid enhanced base64")

var decodeTable = newDecodeTable()

func newDecodeTable() [256]int8 {
	var table [256]int8
	for i := range table {
		table[i] = -1
	}
	for i := range len(Alphabet) {
		table[Alphabet[i]] = int8(i)
	}
	return table
}

func Encode(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	out := make([]byte, 0, (len(src)*8+5)/6)
	var buf uint32
	var bits uint
	for _, b := range src {
		buf = buf<<8 | uint32(b)
		bits += 8
		for bits >= 6 {
			bits -= 6
			out = append(out, Alphabet[(buf>>bits)&0x3f])
		}
	}
	if bits > 0 {
		out = append(out, Alphabet[(buf<<(6-bits))&0x3f])
	}
	return string(out)
}

func Decode(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	out := make([]byte, 0, len(s)*6/8)
	var buf uint32
	var bits uint
	for i := range len(s) {
		v := decodeTable[s[i]]
		if v < 0 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidBase64, s[i])
		}
		buf = buf<<6 | uint32(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte((buf>>bits)&0xff))
		}
	}
	return out, nil
}

const cardinalSymbols = 10

func cardinal(s string) (uint64, error) {
	n := min(len(s), cardinalSymbols)
	var c uint64
	for i := range n {
		v := decodeTable[s[i]]
		if v < 0 {
			return 0, fmt.Errorf("%w: %q", ErrInvalidBase64, s[i])
		}
		c = c<<6 | uint64(v)
	}
	for range cardinalSymbols - n {
		c <<= 6
	}
	return c<<3 | 7, nil
}
