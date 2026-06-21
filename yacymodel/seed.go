package yacymodel

import (
	"errors"
	"fmt"
	"net"
	"net/url"
)

var ErrBadSeed = errors.New("bad seed")

type Seed struct {
	Hash                Hash
	Name                Optional[string]
	IP                  Optional[Host]
	IP6                 Optional[Host]
	Port                Optional[Port]
	PortSSL             Optional[Port]
	PeerType            Optional[PeerType]
	Flags               Optional[Flags]
	Version             Optional[string]
	Uptime              Optional[int]
	UTC                 Optional[string]
	LastSeen            Optional[string]
	RWICount            Optional[int]
	URLCount            Optional[int]
	IndexOut            Optional[int64]
	IndexIn             Optional[int64]
	URLOut              Optional[int64]
	URLIn               Optional[int64]
	UploadSpeed         Optional[int64]
	BirthDate           Optional[string]
	IndexSpeed          Optional[int64]
	RetrievalSpeed      Optional[float64]
	NoticedURLCount     Optional[int64]
	RemoteCrawlURLCount Optional[int64]
	SeedCount           Optional[int64]
	ClientConnectCount  Optional[float64]
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
