package yacymodel

const (
	SeedNoticedURLCount = "NCount"
	SeedOfferedURLCount = "RCount"
	SeedKnownSeedCount  = "SCount"
	SeedConnectsPerHour = "CCount"
	SeedIndexingSpeed   = "ISpeed"
	SeedRequestSpeed    = "RSpeed"
	SeedUplinkSpeed     = "USpeed"
)

func putSeedStatistics(fields map[string]string, s Seed) {
	putInt(fields, SeedNoticedURLCount, s.NoticedURLCount)
	putInt(fields, SeedOfferedURLCount, s.OfferedURLCount)
	putInt(fields, SeedKnownSeedCount, s.KnownSeedCount)
	putInt(fields, SeedConnectsPerHour, s.ConnectsPerHour)
	putInt(fields, SeedIndexingSpeed, s.IndexingSpeed)
	putInt(fields, SeedRequestSpeed, s.RequestSpeed)
	putInt(fields, SeedUplinkSpeed, s.UplinkSpeed)
}
