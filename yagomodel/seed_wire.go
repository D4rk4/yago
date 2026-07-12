package yagomodel

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	SeedHash      = "Hash"
	SeedName      = "Name"
	SeedIP        = "IP"
	SeedIP6       = "IP6"
	SeedPort      = "Port"
	SeedPortSSL   = "PortSSL"
	SeedPeerType  = "PeerType"
	SeedFlags     = "Flags"
	SeedVersion   = "Version"
	SeedUptime    = "Uptime"
	SeedUTC       = "UTC"
	SeedBirthDate = "BDate"
	SeedLastSeen  = "LastSeen"
	SeedRWICount  = "ICount"
	SeedURLCount  = "LCount"
	SeedNews      = "news"
)

func ParseSeed(ctx context.Context, s string) (Seed, error) {
	if len(s) > maximumSeedPlainBytes {
		return Seed{}, fmt.Errorf(
			"%w: seed exceeds %d bytes",
			ErrBadSeed,
			maximumSeedPlainBytes,
		)
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = s[1 : len(s)-1]
	}
	var seed Seed
	hashSet := false
	properties := 0
	for pair := range strings.SplitSeq(s, ",") {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		properties++
		if properties > maximumSeedProperties {
			return Seed{}, fmt.Errorf(
				"%w: seed exceeds %d properties",
				ErrBadSeed,
				maximumSeedProperties,
			)
		}
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return Seed{}, fmt.Errorf("%w: malformed field %q", ErrBadSeed, pair)
		}
		if len(key) > maximumSeedPropertyKey {
			return Seed{}, fmt.Errorf(
				"%w: seed property key exceeds %d bytes",
				ErrBadSeed,
				maximumSeedPropertyKey,
			)
		}
		if limit := seedPropertyLimit(key); len(value) > limit {
			return Seed{}, fmt.Errorf(
				"%w: seed property %s exceeds %d bytes",
				ErrBadSeed,
				key,
				limit,
			)
		}
		key = strings.Clone(key)
		value = strings.Clone(value)
		if key == SeedHash {
			hashSet = true
		}
		if err := parseSeedField(&seed, key, value); err != nil {
			return Seed{}, err
		}
	}
	if !hashSet {
		return Seed{}, fmt.Errorf("%w: missing %s", ErrBadSeed, SeedHash)
	}
	return seed, nil
}

func ParseSeedWireForm(ctx context.Context, form string) (Seed, error) {
	plain, err := DecodeWireFormWithLimit(ctx, form, maximumSeedPlainBytes)
	if err != nil {
		return Seed{}, fmt.Errorf("seed wire form: %w", err)
	}

	return ParseSeed(ctx, plain)
}

func parseSeedField(seed *Seed, key, value string) error {
	if ok, err := parseCoreSeedField(seed, key, value); ok {
		return seedFieldError(key, err)
	}
	if seed.customProperties == nil {
		seed.customProperties = map[string]string{}
	}
	seed.customProperties[key] = value
	return nil
}

func parseCoreSeedField(seed *Seed, key, value string) (bool, error) {
	var err error
	switch key {
	case SeedHash:
		seed.Hash, err = ParseHash(value)
	case SeedName:
		seed.Name = Some(value)
	case SeedIP:
		if value != "" {
			err = parseInto(&seed.IP, ParseHost, value)
		}
	case SeedIP6:
		if value != "" {
			err = parseInto(&seed.IP6, ParseIP6, value)
		}
	case SeedPort:
		err = parseInto(&seed.Port, ParsePort, value)
	case SeedPortSSL:
		err = parseInto(&seed.PortSSL, ParsePort, value)
	case SeedPeerType:
		err = parseInto(&seed.PeerType, ParsePeerType, value)
	case SeedFlags:
		err = parseInto(&seed.Flags, ParseFlags, value)
	case SeedVersion:
		err = parseInto(&seed.Version, ParseYaCyVersion, value)
	case SeedUptime:
		err = parseInto(&seed.Uptime, strconv.Atoi, value)
	case SeedUTC:
		err = parseInto(&seed.UTC, ParseSeedUTC, value)
	case SeedBirthDate:
		err = parseInto(&seed.BirthDate, ParseSeedBirthDateUTC, value)
	case SeedLastSeen:
		err = parseInto(&seed.LastSeen, ParseSeedLastSeenUTC, value)
	case SeedRWICount:
		err = parseInto(&seed.RWICount, strconv.Atoi, value)
	case SeedURLCount:
		err = parseInto(&seed.URLCount, strconv.Atoi, value)
	default:
		return false, nil
	}
	return true, err
}

func seedFieldError(key string, err error) error {
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
	fields := s.Properties()

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

func (s Seed) Properties() map[string]string {
	fields := map[string]string{SeedHash: s.Hash.String()}
	putStringer(fields, SeedIP, s.IP)
	putHostList(fields, SeedIP6, s.IP6)
	putStringer(fields, SeedPort, s.Port)
	putStringer(fields, SeedPortSSL, s.PortSSL)
	putStringer(fields, SeedPeerType, s.PeerType)
	putStringer(fields, SeedFlags, s.Flags)
	putText(fields, SeedName, s.Name)
	putStringer(fields, SeedVersion, s.Version)
	putStringer(fields, SeedUTC, s.UTC)
	putStringer(fields, SeedBirthDate, s.BirthDate)
	putStringer(fields, SeedLastSeen, s.LastSeen)
	putInt(fields, SeedUptime, s.Uptime)
	putInt(fields, SeedRWICount, s.RWICount)
	putInt(fields, SeedURLCount, s.URLCount)
	putText(fields, SeedNews, s.News)
	putSeedStatistics(fields, s)
	for key, value := range s.customProperties {
		if _, ok := fields[key]; !ok {
			fields[key] = value
		}
	}

	return fields
}

func putStringer[T fmt.Stringer](fields map[string]string, key string, opt Optional[T]) {
	if v, ok := opt.Get(); ok {
		fields[key] = v.String()
	}
}

func putHostList(fields map[string]string, key string, opt Optional[[]Host]) {
	if hosts, ok := opt.Get(); ok {
		values := make([]string, 0, len(hosts))
		for _, host := range hosts {
			values = append(values, host.String())
		}
		fields[key] = strings.Join(values, "|")
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
