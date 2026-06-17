package yacywire

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

// Seed field keys carried in a seed string.
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

// ErrBadSeed reports a seed string that cannot be parsed.
var ErrBadSeed = errors.New("bad seed")

// Seed is a peer's seed: a map of string fields. Its wire form is a
// comma-separated list of sorted Key=Value pairs.
type Seed map[string]string

// ParseSeed parses a plaintext seed string. It returns ErrBadSeed when a pair
// lacks a key.
func ParseSeed(s string) (Seed, error) {
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

// String returns the plaintext seed string with keys in sorted order.
func (s Seed) String() string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(s[k])
	}
	return b.String()
}

// Hash returns the peer hash field validated as a Hash.
func (s Seed) Hash() (Hash, error) {
	return ParseHash(s[SeedHash])
}

// PeerType returns the peer type field validated as a PeerType.
func (s Seed) PeerType() (PeerType, error) {
	return ParsePeerType(s[SeedPeerType])
}

// Flags returns the flags field validated as Flags.
func (s Seed) Flags() (Flags, error) {
	return ParseFlags(s[SeedFlags])
}

// Port returns the peer port field parsed as an integer.
func (s Seed) Port() (int, error) {
	n, err := strconv.Atoi(s[SeedPort])
	if err != nil {
		return 0, ErrBadSeed
	}
	return n, nil
}
