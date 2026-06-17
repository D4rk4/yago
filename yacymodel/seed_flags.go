package yacymodel

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

var ErrInvalidFlags = errors.New("invalid flags")

type Flags [FlagsLength]byte

func ZeroFlags() Flags {
	return Flags([]byte(FlagsZero))
}

func ParseFlags(s string) (Flags, error) {
	if len(s) != FlagsLength {
		return Flags{}, fmt.Errorf("%w: length %d, want %d", ErrInvalidFlags, len(s), FlagsLength)
	}
	return Flags([]byte(s)), nil
}

func (f Flags) String() string { return string(f[:]) }

func (f Flags) Get(bit int) bool {
	return f[bit>>3]&(1<<(bit&7)) != 0
}

func (f Flags) Set(bit int, value bool) Flags {
	if value {
		f[bit>>3] |= 1 << (bit & 7)
	} else {
		f[bit>>3] &^= 1 << (bit & 7)
	}
	return f
}
