package yacymodel

import (
	"errors"
	"fmt"
	"time"
)

const seedLastSeenLayout = "20060102150405"

var ErrBadSeedLastSeenUTC = errors.New("bad seed last seen utc")

type SeedLastSeenUTC struct {
	value time.Time
}

func ParseSeedLastSeenUTC(s string) (SeedLastSeenUTC, error) {
	parsed, err := time.ParseInLocation(seedLastSeenLayout, s, time.UTC)
	if err != nil {
		return SeedLastSeenUTC{}, fmt.Errorf("%w: %w", ErrBadSeedLastSeenUTC, err)
	}
	return SeedLastSeenUTC{value: parsed}, nil
}

func NewSeedLastSeenUTC(t time.Time) SeedLastSeenUTC {
	return SeedLastSeenUTC{value: t.UTC().Truncate(time.Second)}
}

func (s SeedLastSeenUTC) Time() time.Time {
	return s.value
}

func (s SeedLastSeenUTC) String() string {
	return s.value.UTC().Format(seedLastSeenLayout)
}
