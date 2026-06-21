package yacymodel

import (
	"context"
	"fmt"
	"log/slog"
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
	SeedFlags    = "Flags"
	SeedVersion  = "Version"
	SeedUptime   = "Uptime"
	SeedUTC      = "UTC"
	SeedLastSeen = "LastSeen"
	SeedRWICount = "ICount"
	SeedURLCount = "LCount"
)

func ParseSeed(ctx context.Context, s string) (Seed, error) {
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = s[1 : len(s)-1]
	}
	var seed Seed
	hashSet := false
	for pair := range strings.SplitSeq(s, ",") {
		if pair == "" {
			continue
		}
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return Seed{}, ErrBadSeed
		}
		if key == SeedHash {
			hashSet = true
		}
		if err := parseSeedField(ctx, &seed, key, value); err != nil {
			return Seed{}, err
		}
	}
	if !hashSet {
		return Seed{}, fmt.Errorf("%w: missing %s", ErrBadSeed, SeedHash)
	}
	return seed, nil
}

func parseSeedField(ctx context.Context, seed *Seed, key, value string) error {
	var err error
	switch key {
	case SeedHash:
		seed.Hash, err = ParseHash(value)
	case SeedName:
		seed.Name = Some(value)
	case SeedIP:
		err = parseInto(&seed.IP, ParseHost, value)
	case SeedIP6:
		err = parseInto(&seed.IP6, ParseHost, value)
	case SeedPort:
		err = parseInto(&seed.Port, ParsePort, value)
	case SeedPortSSL:
		err = parseInto(&seed.PortSSL, ParsePort, value)
	case SeedPeerType:
		err = parseInto(&seed.PeerType, ParsePeerType, value)
	case SeedFlags:
		err = parseInto(&seed.Flags, ParseFlags, value)
	case SeedVersion:
		seed.Version = Some(value)
	case SeedUptime:
		err = parseInto(&seed.Uptime, strconv.Atoi, value)
	case SeedUTC:
		seed.UTC = Some(value)
	case SeedLastSeen:
		seed.LastSeen = Some(value)
	case SeedRWICount:
		err = parseInto(&seed.RWICount, strconv.Atoi, value)
	case SeedURLCount:
		err = parseInto(&seed.URLCount, strconv.Atoi, value)
	default:
		slog.WarnContext(ctx, "unknown seed field ignored", slog.String("field", key))
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrBadSeed, key, err)
	}
	return nil
}

func parseInto[T any](target *Optional[T], parse func(string) (T, error), value string) error {
	parsed, err := parse(value)
	if err != nil {
		return err
	}
	*target = Some(parsed)
	return nil
}

func (s Seed) String() string {
	fields := map[string]string{SeedHash: s.Hash.String()}
	putStringer(fields, SeedIP, s.IP)
	putStringer(fields, SeedIP6, s.IP6)
	putStringer(fields, SeedPort, s.Port)
	putStringer(fields, SeedPortSSL, s.PortSSL)
	putStringer(fields, SeedPeerType, s.PeerType)
	putStringer(fields, SeedFlags, s.Flags)
	putText(fields, SeedName, s.Name)
	putText(fields, SeedVersion, s.Version)
	putText(fields, SeedUTC, s.UTC)
	putText(fields, SeedLastSeen, s.LastSeen)
	putInt(fields, SeedUptime, s.Uptime)
	putInt(fields, SeedRWICount, s.RWICount)
	putInt(fields, SeedURLCount, s.URLCount)

	keys := make([]string, 0, len(fields))
	for k := range fields {
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
		b.WriteString(fields[k])
	}
	b.WriteByte('}')
	return b.String()
}

func putStringer[T fmt.Stringer](fields map[string]string, key string, opt Optional[T]) {
	if v, ok := opt.Get(); ok {
		fields[key] = v.String()
	}
}

func putText(fields map[string]string, key string, opt Optional[string]) {
	if v, ok := opt.Get(); ok {
		fields[key] = v
	}
}

func putInt(fields map[string]string, key string, opt Optional[int]) {
	if v, ok := opt.Get(); ok {
		fields[key] = strconv.Itoa(v)
	}
}
