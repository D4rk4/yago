package yacymodel

import "strconv"

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

func putInt64(fields map[string]string, key string, opt Optional[int64]) {
	if v, ok := opt.Get(); ok {
		fields[key] = strconv.FormatInt(v, 10)
	}
}
