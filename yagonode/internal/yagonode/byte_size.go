package yagonode

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var byteSizeUnits = []struct {
	suffix string
	factor int64
}{
	{"TB", 1 << 40},
	{"GB", 1 << 30},
	{"MB", 1 << 20},
	{"KB", 1 << 10},
	{"B", 1},
}

func parseByteSize(raw string) (int64, error) {
	text := strings.ToUpper(strings.TrimSpace(raw))
	for _, unit := range byteSizeUnits {
		if !strings.HasSuffix(text, unit.suffix) {
			continue
		}
		digits := strings.TrimSpace(strings.TrimSuffix(text, unit.suffix))
		value, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q: %w", raw, err)
		}
		if value < 0 {
			return 0, fmt.Errorf("invalid size %q: must not be negative", raw)
		}
		if value > math.MaxInt64/unit.factor {
			return 0, fmt.Errorf("invalid size %q: exceeds supported range", raw)
		}

		return value * unit.factor, nil
	}

	return 0, fmt.Errorf("invalid size %q: missing unit suffix", raw)
}
