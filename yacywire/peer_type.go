package yacywire

import (
	"errors"
	"fmt"
)

// PeerType classifies a peer's network reachability and role.
type PeerType string

// The peer types YaCy advertises in a seed's PeerType field.
const (
	PeerVirgin    PeerType = "virgin"
	PeerJunior    PeerType = "junior"
	PeerSenior    PeerType = "senior"
	PeerPrincipal PeerType = "principal"
)

// ErrInvalidPeerType reports a value that is not a known peer type.
var ErrInvalidPeerType = errors.New("invalid peer type")

// ParsePeerType validates s and returns it as a PeerType.
func ParsePeerType(s string) (PeerType, error) {
	switch PeerType(s) {
	case PeerVirgin, PeerJunior, PeerSenior, PeerPrincipal:
		return PeerType(s), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidPeerType, s)
	}
}

// String returns t as a string.
func (t PeerType) String() string { return string(t) }
