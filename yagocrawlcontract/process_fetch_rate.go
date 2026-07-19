package yagocrawlcontract

const (
	DefaultProcessPagesPerSecond = 10
	MaximumProcessPagesPerSecond = 1_000_000
)

func ParseProcessPagesPerSecond(raw string) (uint32, error) {
	return parseBoundedUint32(
		raw,
		"process pages per second",
		MaximumProcessPagesPerSecond,
	)
}
