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
	hosts := s.AdvertisedHosts()
	if len(hosts) == 0 {
		return "", false
	}
	port, ok := s.Port.Get()
	if !ok {
		return "", false
	}
	return net.JoinHostPort(hosts[0].String(), port.String()), true
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

// minimumSSLVersion is the release that introduced the SSL seed flag; YaCy's
// Seed.getFlagSSLAvailable treats older peers as flagless because their flag
// bit carried no meaning.
const minimumSSLVersion = 1.5

// SSLAvailable reports whether the peer advertises a usable HTTPS peer-protocol
// endpoint: the seed's SSL flag is set, an SSL port is present, and the peer
// version is recent enough for the flag bit to mean SSL.
func (s Seed) SSLAvailable() bool {
	version, ok := s.Version.Get()
	if !ok {
		return false
	}
	value, err := version.Float()
	if err != nil || value < minimumSSLVersion {
		return false
	}
	flags, ok := s.Flags.Get()
	if !ok || !flags.Get(FlagSSLAvailable) {
		return false
	}
	_, ok = s.PortSSL.Get()

	return ok
}

func (s Seed) ProtocolEndpoints(path string, preferHTTPS bool) ([]*url.URL, error) {
	hosts := s.AdvertisedHosts()
	plainPort, plainPortKnown := s.Port.Get()
	if len(hosts) == 0 || !plainPortKnown {
		return nil, fmt.Errorf("%w: no reachable address", ErrBadSeed)
	}
	secureAvailable := preferHTTPS && s.SSLAvailable()
	securePort, _ := s.PortSSL.Get()
	capacity := len(hosts)
	if secureAvailable {
		capacity *= 2
	}
	endpoints := make([]*url.URL, 0, capacity)
	for _, host := range hosts {
		if secureAvailable {
			endpoints = append(endpoints, &url.URL{
				Scheme: "https",
				Host:   net.JoinHostPort(host.String(), securePort.String()),
				Path:   path,
			})
		}
		endpoints = append(endpoints, &url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(host.String(), plainPort.String()),
			Path:   path,
		})
	}

	return endpoints, nil
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
