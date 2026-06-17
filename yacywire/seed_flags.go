package yacywire

import (
	"errors"
	"fmt"
)

// FlagsLength is the fixed width, in characters, of a seed Flags field.
const FlagsLength = 4

// FlagsZero is the default seed Flags value: four spaces, which keeps every
// byte printable while all defined flag bits are clear.
const FlagsZero = "    "

// Seed Flags bit positions, counted from the least-significant bit of the
// first atom.
const (
	FlagDirectConnect = iota
	FlagAcceptRemoteCrawl
	FlagAcceptRemoteIndex
	FlagRootNode
	FlagSSLAvailable
)

// ErrInvalidFlags reports a Flags field that is not FlagsLength characters wide.
var ErrInvalidFlags = errors.New("invalid flags")

// Flags is a seed's printable bitfield. Each character carries eight bits; bit
// n lives in atom n/8 at position n%8.
type Flags [FlagsLength]byte

// ZeroFlags returns the default all-clear Flags.
func ZeroFlags() Flags {
	return Flags([]byte(FlagsZero))
}

// ParseFlags validates s and returns it as Flags.
func ParseFlags(s string) (Flags, error) {
	if len(s) != FlagsLength {
		return Flags{}, fmt.Errorf("%w: length %d, want %d", ErrInvalidFlags, len(s), FlagsLength)
	}
	return Flags([]byte(s)), nil
}

// String returns the wire form of f.
func (f Flags) String() string { return string(f[:]) }

// Get reports whether bit is set.
func (f Flags) Get(bit int) bool {
	return f[bit>>3]&(1<<(bit&7)) != 0
}

// Set returns a copy of f with bit set to value.
func (f Flags) Set(bit int, value bool) Flags {
	if value {
		f[bit>>3] |= 1 << (bit & 7)
	} else {
		f[bit>>3] &^= 1 << (bit & 7)
	}
	return f
}
