package yagomodel

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type YaCyVersion string

func ParseYaCyVersion(s string) (YaCyVersion, error) {
	return YaCyVersion(s), nil
}

func (v YaCyVersion) String() string {
	return string(v)
}

func (v YaCyVersion) Float() (float64, error) {
	raw := string(v)
	trimmed := strings.TrimFunc(raw, func(r rune) bool { return r <= ' ' })

	switch trimmed {
	case "NaN", "+NaN", "-NaN":
		return math.NaN(), nil
	case "Infinity", "+Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	}

	if invalidYaCyVersionSpecial(trimmed) || strings.Contains(trimmed, "_") {
		return 0, fmt.Errorf("parse yacy version %q", raw)
	}
	if strings.HasSuffix(trimmed, "f") || strings.HasSuffix(trimmed, "F") ||
		strings.HasSuffix(trimmed, "d") ||
		strings.HasSuffix(trimmed, "D") {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if invalidYaCyVersionSpecial(trimmed) {
		return 0, fmt.Errorf("parse yacy version %q", raw)
	}

	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, fmt.Errorf("parse yacy version %q: %w", raw, err)
	}

	return value, nil
}

func invalidYaCyVersionSpecial(raw string) bool {
	unsigned := strings.TrimPrefix(strings.TrimPrefix(raw, "+"), "-")

	return strings.EqualFold(unsigned, "NaN") ||
		strings.EqualFold(unsigned, "Inf") ||
		strings.EqualFold(unsigned, "Infinity")
}
