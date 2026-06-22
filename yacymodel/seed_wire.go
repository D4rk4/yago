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
	SeedHash                = "Hash"
	SeedName                = "Name"
	SeedIP                  = "IP"
	SeedIP6                 = "IP6"
	SeedPort                = "Port"
	SeedPortSSL             = "PortSSL"
	SeedPeerType            = "PeerType"
	SeedFlags               = "Flags"
	SeedVersion             = "Version"
	SeedUptime              = "Uptime"
	SeedUTC                 = "UTC"
	SeedLastSeen            = "LastSeen"
	SeedRWICount            = "ICount"
	SeedURLCount            = "LCount"
	SeedIndexOut            = "sI"
	SeedIndexIn             = "rI"
	SeedURLOut              = "sU"
	SeedURLIn               = "rU"
	SeedUploadSpeed         = "USpeed"
	SeedBirthDate           = "BDate"
	SeedIndexSpeed          = "ISpeed"
	SeedRetrievalSpeed      = "RSpeed"
	SeedNoticedURLCount     = "NCount"
	SeedRemoteCrawlURLCount = "RCount"
	SeedCount               = "SCount"
	SeedClientConnectCount  = "CCount"
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
	if ok, err := parseCoreSeedField(seed, key, value); ok {
		return seedFieldError(key, err)
	}
	if ok, err := parseTrafficSeedField(seed, key, value); ok {
		return seedFieldError(key, err)
	}
	slog.WarnContext(ctx, "unknown seed field ignored", slog.String("field", key))
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
		return false, nil
	}
	return true, err
}

func parseTrafficSeedField(seed *Seed, key, value string) (bool, error) {
	var err error
	switch key {
	case SeedIndexOut:
		err = parseInto(&seed.IndexOut, parseInt64, value)
	case SeedIndexIn:
		err = parseInto(&seed.IndexIn, parseInt64, value)
	case SeedURLOut:
		err = parseInto(&seed.URLOut, parseInt64, value)
	case SeedURLIn:
		err = parseInto(&seed.URLIn, parseInt64, value)
	case SeedUploadSpeed:
		err = parseInto(&seed.UploadSpeed, parseInt64, value)
	case SeedBirthDate:
		seed.BirthDate = Some(value)
	case SeedIndexSpeed:
		err = parseInto(&seed.IndexSpeed, parseInt64, value)
	case SeedRetrievalSpeed:
		err = parseInto(&seed.RetrievalSpeed, parseFloat64, value)
	case SeedNoticedURLCount:
		err = parseInto(&seed.NoticedURLCount, parseInt64, value)
	case SeedRemoteCrawlURLCount:
		err = parseInto(&seed.RemoteCrawlURLCount, parseInt64, value)
	case SeedCount:
		err = parseInto(&seed.SeedCount, parseInt64, value)
	case SeedClientConnectCount:
		err = parseInto(&seed.ClientConnectCount, parseFloat64, value)
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

func parseInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse int64: %w", err)
	}
	return parsed, nil
}

func parseFloat64(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float64: %w", err)
	}
	return parsed, nil
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
	putInt64(fields, SeedIndexOut, s.IndexOut)
	putInt64(fields, SeedIndexIn, s.IndexIn)
	putInt64(fields, SeedURLOut, s.URLOut)
	putInt64(fields, SeedURLIn, s.URLIn)
	putInt64(fields, SeedUploadSpeed, s.UploadSpeed)
	putText(fields, SeedBirthDate, s.BirthDate)
	putInt64(fields, SeedIndexSpeed, s.IndexSpeed)
	putFloat64(fields, SeedRetrievalSpeed, s.RetrievalSpeed)
	putInt64(fields, SeedNoticedURLCount, s.NoticedURLCount)
	putInt64(fields, SeedRemoteCrawlURLCount, s.RemoteCrawlURLCount)
	putInt64(fields, SeedCount, s.SeedCount)
	putFloat64(fields, SeedClientConnectCount, s.ClientConnectCount)

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

func putInt64(fields map[string]string, key string, opt Optional[int64]) {
	if v, ok := opt.Get(); ok {
		fields[key] = strconv.FormatInt(v, 10)
	}
}

func putFloat64(fields map[string]string, key string, opt Optional[float64]) {
	if v, ok := opt.Get(); ok {
		fields[key] = strconv.FormatFloat(v, 'f', -1, 64)
	}
}
