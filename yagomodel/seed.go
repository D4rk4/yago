package yagomodel

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"
)

var ErrBadSeed = errors.New("bad seed")

type Seed struct {
	Hash              Hash
	Name              Optional[string]
	IP                Optional[Host]
	IP6               Optional[[]Host]
	Port              Optional[Port]
	PortSSL           Optional[Port]
	PeerType          Optional[PeerType]
	Flags             Optional[Flags]
	Version           Optional[YaCyVersion]
	Uptime            Optional[int]
	UTC               Optional[SeedUTCValue]
	BirthDate         Optional[SeedBirthDateUTC]
	LastSeen          Optional[SeedLastSeenUTC]
	RWICount          Optional[int]
	URLCount          Optional[int]
	NoticedURLCount   Optional[int]
	OfferedURLCount   Optional[int]
	KnownSeedCount    Optional[int]
	ConnectsPerHour   Optional[int]
	IndexingSpeed     Optional[int]
	RequestSpeed      Optional[int]
	UplinkSpeed       Optional[int]
	SentWordCount     Optional[int64]
	ReceivedWordCount Optional[int64]
	SentURLCount      Optional[int64]
	ReceivedURLCount  Optional[int64]
	News              Optional[string]
	customProperties  map[string]string
}

func (s Seed) NetworkAddress() (string, bool) {
	host, ok := s.IP.Get()
	if !ok {
		return "", false
	}
	port, ok := s.Port.Get()
	if !ok {
		return "", false
	}
	return net.JoinHostPort(host.String(), port.String()), true
}

func (s Seed) HTTPEndpoint(path string) (*url.URL, error) {
	address, ok := s.NetworkAddress()
	if !ok {
		return nil, fmt.Errorf("%w: no reachable address", ErrBadSeed)
	}

	return &url.URL{
		Scheme: "http",
		Host:   address,
		Path:   path,
	}, nil
}

func (s Seed) AgeDays(now time.Time) int {
	birth := time.Date(2004, 1, 1, 0, 0, 0, 0, time.UTC)
	if value, ok := s.BirthDate.Get(); ok {
		birth = value.Time()
	}

	delta := now.UTC().Sub(birth)
	if delta < 0 {
		delta = -delta
	}

	return int(delta / (24 * time.Hour))
}
