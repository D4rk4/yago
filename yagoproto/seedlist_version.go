package yagoproto

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

func parseSeedlistVersion(raw string) (float64, bool) {
	trimmed := strings.TrimFunc(raw, func(r rune) bool { return r <= ' ' })

	switch trimmed {
	case "NaN", "+NaN", "-NaN":
		return math.NaN(), true
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	}

	special := strings.TrimPrefix(strings.TrimPrefix(trimmed, "+"), "-")
	if strings.EqualFold(special, "NaN") || strings.EqualFold(special, "Inf") ||
		strings.EqualFold(special, "Infinity") {
		return 0, false
	}
	if strings.Contains(trimmed, "_") {
		return 0, false
	}
	if strings.HasSuffix(trimmed, "f") || strings.HasSuffix(trimmed, "F") ||
		strings.HasSuffix(trimmed, "d") ||
		strings.HasSuffix(trimmed, "D") {
		trimmed = trimmed[:len(trimmed)-1]
	}
	special = strings.TrimPrefix(strings.TrimPrefix(trimmed, "+"), "-")
	if strings.EqualFold(special, "NaN") || strings.EqualFold(special, "Inf") ||
		strings.EqualFold(special, "Infinity") {
		return 0, false
	}

	value, err := strconv.ParseFloat(trimmed, 32)
	if err == nil || errors.Is(err, strconv.ErrRange) {
		return value, true
	}

	return 0, false
}
