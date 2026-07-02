package yacymodel

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrBadSeedUTC = errors.New("bad seed utc")

type SeedUTCValue string

func ParseSeedUTC(s string) (SeedUTCValue, error) {
	if seedUTCOffsetValid(s) || seedUTCTimestampValid(s) {
		return SeedUTCValue(s), nil
	}

	return "", fmt.Errorf("%w: %q", ErrBadSeedUTC, s)
}

func SeedUTCOffsetFromTime(t time.Time) SeedUTCValue {
	_, offsetSeconds := t.Zone()
	sign := '+'
	if offsetSeconds < 0 {
		sign = '-'
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / int(time.Hour/time.Second)
	minutes := offsetSeconds / int(time.Minute/time.Second) % 60
	return SeedUTCValue(fmt.Sprintf("%c%02d%02d", sign, hours, minutes))
}

func (u SeedUTCValue) String() string {
	return string(u)
}

func seedUTCOffsetValid(s string) bool {
	if len(s) != 5 || s[0] != '+' && s[0] != '-' {
		return false
	}
	hours, err := strconv.Atoi(s[1:3])
	if err != nil {
		return false
	}
	minutes, err := strconv.Atoi(s[3:5])
	if err != nil {
		return false
	}

	return hours <= 23 && minutes <= 59
}

func seedUTCTimestampValid(s string) bool {
	_, err := time.ParseInLocation(seedTimestampLayout, s, time.UTC)

	return err == nil
}
