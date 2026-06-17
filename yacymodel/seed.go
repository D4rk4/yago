package yacymodel

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

const (
	SeedHash     = "Hash"
	SeedName     = "Name"
	SeedIP       = "IP"
	SeedIP6      = "IP6"
	SeedPort     = "Port"
	SeedPortSSL  = "PortSSL"
	SeedPeerType = "PeerType"
	SeedVersion  = "Version"
	SeedUptime   = "Uptime"
	SeedUTC      = "UTC"
	SeedLastSeen = "LastSeen"
	SeedFlags    = "Flags"
)

var ErrBadSeed = errors.New("bad seed")

type Seed map[string]string

func ParseSeed(s string) (Seed, error) {
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = s[1 : len(s)-1]
	}
	seed := make(Seed)
	for pair := range strings.SplitSeq(s, ",") {
		if pair == "" {
			continue
		}
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, ErrBadSeed
		}
		seed[key] = value
	}
	return seed, nil
}

func (s Seed) String() string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(s[k])
	}
	b.WriteByte('}')
	return b.String()
}

func (s Seed) Hash() (Hash, error) {
	return ParseHash(s[SeedHash])
}

func (s Seed) PeerType() (PeerType, error) {
	return ParsePeerType(s[SeedPeerType])
}

func (s Seed) Flags() (Flags, error) {
	return ParseFlags(s[SeedFlags])
}

func (s Seed) Port() (int, error) {
	n, err := strconv.Atoi(s[SeedPort])
	if err != nil {
		return 0, ErrBadSeed
	}
	return n, nil
}
