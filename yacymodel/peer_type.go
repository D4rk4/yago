package yacymodel

import (
	"errors"
	"fmt"
)

type PeerType string

const (
	PeerVirgin    PeerType = "virgin"
	PeerJunior    PeerType = "junior"
	PeerMentee    PeerType = "mentee"
	PeerSenior    PeerType = "senior"
	PeerMentor    PeerType = "mentor"
	PeerPrincipal PeerType = "principal"
)

var ErrInvalidPeerType = errors.New("invalid peer type")

func ParsePeerType(s string) (PeerType, error) {
	switch PeerType(s) {
	case PeerVirgin, PeerJunior, PeerMentee, PeerSenior, PeerMentor, PeerPrincipal:
		return PeerType(s), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidPeerType, s)
	}
}

func (t PeerType) String() string { return string(t) }
