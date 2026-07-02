package yacymodel

import (
	"errors"
	"fmt"
	"time"
)

var ErrBadSeedBirthDateUTC = errors.New("bad seed birth date utc")

type SeedBirthDateUTC struct {
	value time.Time
}

func ParseSeedBirthDateUTC(s string) (SeedBirthDateUTC, error) {
	parsed, err := time.ParseInLocation(seedTimestampLayout, s, time.UTC)
	if err != nil {
		return SeedBirthDateUTC{}, fmt.Errorf("%w: %w", ErrBadSeedBirthDateUTC, err)
	}

	return SeedBirthDateUTC{value: parsed}, nil
}

func NewSeedBirthDateUTC(t time.Time) SeedBirthDateUTC {
	return SeedBirthDateUTC{value: t.UTC().Truncate(time.Second)}
}

func (s SeedBirthDateUTC) Time() time.Time {
	return s.value
}

func (s SeedBirthDateUTC) String() string {
	return s.value.UTC().Format(seedTimestampLayout)
}
