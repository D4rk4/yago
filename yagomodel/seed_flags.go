package yagomodel

import (
	"errors"
	"fmt"
)

const FlagsLength = 4

const FlagsZero = "    "

const (
	FlagDirectConnect = iota
	FlagAcceptRemoteCrawl
	FlagAcceptRemoteIndex
	FlagRootNode
	FlagSSLAvailable
)

const (
	flagBitsPerAtom      = 5
	flagASCIIOffset byte = ' '
)

var ErrInvalidFlags = errors.New("invalid flags")

type Flags [FlagsLength]byte

func ZeroFlags() Flags {
	return Flags([]byte(FlagsZero))
}

func ParseFlags(s string) (Flags, error) {
	if len(s) > FlagsLength {
		return Flags{}, fmt.Errorf(
			"%w: length %d, want at most %d",
			ErrInvalidFlags,
			len(s),
			FlagsLength,
		)
	}
	f := ZeroFlags()
	copy(f[:], s)
	return f, nil
}

func (f Flags) String() string { return string(f[:]) }

func (f Flags) Get(bit int) bool {
	atoms := f[:]
	slot := bit / flagBitsPerAtom
	if bit < 0 || slot >= len(atoms) {
		return false
	}
	mask := byte(1) << (bit % flagBitsPerAtom)
	return (atoms[slot]-flagASCIIOffset)&mask != 0
}

func (f Flags) Set(bit int, value bool) Flags {
	atoms := f[:]
	slot := bit / flagBitsPerAtom
	if bit < 0 || slot >= len(atoms) {
		return f
	}
	mask := byte(1) << (bit % flagBitsPerAtom)
	atom := atoms[slot] - flagASCIIOffset
	if value {
		atom |= mask
	} else {
		atom &^= mask
	}
	atoms[slot] = atom + flagASCIIOffset
	return f
}
