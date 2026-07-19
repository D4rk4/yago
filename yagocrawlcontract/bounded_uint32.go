package yagocrawlcontract

import (
	"fmt"
	"strconv"
	"strings"
)

func parseBoundedUint32(raw, name string, maximum uint64) (uint32, error) {
	value, err := strconv.ParseUint(
		strings.TrimPrefix(strings.TrimSpace(raw), "+"),
		10,
		32,
	)
	if err != nil || value > maximum {
		return 0, fmt.Errorf(
			"%s must be an integer between 0 and %d",
			name,
			maximum,
		)
	}

	return uint32(value), nil
}
