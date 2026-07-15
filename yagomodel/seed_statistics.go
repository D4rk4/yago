package yagomodel

import (
	"fmt"
	"strconv"
)

const (
	SeedNoticedURLCount   = "NCount"
	SeedOfferedURLCount   = "RCount"
	SeedKnownSeedCount    = "SCount"
	SeedConnectsPerHour   = "CCount"
	SeedIndexingSpeed     = "ISpeed"
	SeedRequestSpeed      = "RSpeed"
	SeedUplinkSpeed       = "USpeed"
	SeedSentWordCount     = "sI"
	SeedReceivedWordCount = "rI"
	SeedSentURLCount      = "sU"
	SeedReceivedURLCount  = "rU"
)

func putSeedStatistics(fields map[string]string, s Seed) {
	putInt(fields, SeedNoticedURLCount, s.NoticedURLCount)
	putInt(fields, SeedOfferedURLCount, s.OfferedURLCount)
	putInt(fields, SeedKnownSeedCount, s.KnownSeedCount)
	putInt(fields, SeedConnectsPerHour, s.ConnectsPerHour)
	putInt(fields, SeedIndexingSpeed, s.IndexingSpeed)
	putInt(fields, SeedRequestSpeed, s.RequestSpeed)
	putInt(fields, SeedUplinkSpeed, s.UplinkSpeed)
	putInt64(fields, SeedSentWordCount, s.SentWordCount)
	putInt64(fields, SeedReceivedWordCount, s.ReceivedWordCount)
	putInt64(fields, SeedSentURLCount, s.SentURLCount)
	putInt64(fields, SeedReceivedURLCount, s.ReceivedURLCount)
}

func parseSeedStatisticsField(seed *Seed, key, value string) (bool, error) {
	var err error
	switch key {
	case SeedNoticedURLCount:
		err = parseInto(&seed.NoticedURLCount, strconv.Atoi, value)
	case SeedOfferedURLCount:
		err = parseInto(&seed.OfferedURLCount, strconv.Atoi, value)
	case SeedKnownSeedCount:
		err = parseInto(&seed.KnownSeedCount, strconv.Atoi, value)
	case SeedIndexingSpeed:
		err = parseInto(&seed.IndexingSpeed, strconv.Atoi, value)
	case SeedUplinkSpeed:
		err = parseInto(&seed.UplinkSpeed, strconv.Atoi, value)
	case SeedSentWordCount:
		err = parseInto(&seed.SentWordCount, parseSeedInt64, value)
	case SeedReceivedWordCount:
		err = parseInto(&seed.ReceivedWordCount, parseSeedInt64, value)
	case SeedSentURLCount:
		err = parseInto(&seed.SentURLCount, parseSeedInt64, value)
	case SeedReceivedURLCount:
		err = parseInto(&seed.ReceivedURLCount, parseSeedInt64, value)
	default:
		return false, nil
	}

	return true, err
}

func parseSeedInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse seed statistic: %w", err)
	}

	return parsed, nil
}

func putInt64(fields map[string]string, key string, opt Optional[int64]) {
	if v, ok := opt.Get(); ok {
		fields[key] = strconv.FormatInt(v, 10)
	}
}
