package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var crawlerByteSizeUnits = []struct {
	suffix string
	factor uint64
}{
	{"TB", 1 << 40},
	{"GB", 1 << 30},
	{"MB", 1 << 20},
	{"KB", 1 << 10},
	{"B", 1},
}

func envByteSize(
	getenv func(string) string,
	key string,
	fallback uint64,
) (uint64, error) {
	raw := strings.ToUpper(strings.TrimSpace(getenv(key)))
	if raw == "" {
		return fallback, nil
	}
	for _, unit := range crawlerByteSizeUnits {
		if !strings.HasSuffix(raw, unit.suffix) {
			continue
		}
		digits := strings.TrimSpace(strings.TrimSuffix(raw, unit.suffix))
		value, err := strconv.ParseUint(digits, 10, 64)
		if err != nil || value > math.MaxUint64/unit.factor {
			return 0, fmt.Errorf("%s: invalid byte size %q", key, raw)
		}

		return value * unit.factor, nil
	}

	return 0, fmt.Errorf("%s: invalid byte size %q", key, raw)
}
