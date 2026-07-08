package yacymodel

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrBadSeedUTCOffset = errors.New("bad seed utc offset")

type SeedUTCOffset string

func ParseSeedUTCOffset(s string) (SeedUTCOffset, error) {
	if len(s) != 5 || s[0] != '+' && s[0] != '-' {
		return "", fmt.Errorf("%w: %q", ErrBadSeedUTCOffset, s)
	}
	hours, err := strconv.Atoi(s[1:3])
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadSeedUTCOffset, err)
	}
	minutes, err := strconv.Atoi(s[3:5])
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadSeedUTCOffset, err)
	}
	if hours > 23 || minutes > 59 {
		return "", fmt.Errorf("%w: %q", ErrBadSeedUTCOffset, s)
	}
	return SeedUTCOffset(s), nil
}

func SeedUTCOffsetFromTime(t time.Time) SeedUTCOffset {
	_, offsetSeconds := t.Zone()
	sign := '+'
	if offsetSeconds < 0 {
		sign = '-'
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / int(time.Hour/time.Second)
	minutes := offsetSeconds / int(time.Minute/time.Second) % 60
	return SeedUTCOffset(fmt.Sprintf("%c%02d%02d", sign, hours, minutes))
}

func (o SeedUTCOffset) String() string {
	return string(o)
}
