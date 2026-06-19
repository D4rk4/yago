package yacymodel

import (
	"fmt"
	"strconv"
	"strings"
)

type rwiDecimalPrefix struct {
	magnitude uint64
	negative  bool
}

func normalizeRWIProperties(props map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(props))
	for key, value := range props {
		normalized, err := normalizeRWIProperty(key, value)
		if err != nil {
			return nil, err
		}
		out[key] = normalized
	}
	return out, nil
}

func normalizeRWIProperty(key, value string) (string, error) {
	if _, ok := rwiCardinalWidths[key]; ok {
		n := parseRWIDecimalPrefix(value)
		return strconv.FormatUint(fixedWidthUnsigned(n, rwiCardinalWidths[key]), 10), nil
	}
	switch key {
	case ColDocType, ColWordType:
		n := parseRWIDecimalPrefix(value)
		return strconv.FormatUint(uint64(lowByte(n.unsigned())), 10), nil
	case ColLanguage:
		return clampStringBytes(value, langLength), nil
	case ColFlags:
		raw, err := Decode(value)
		if err != nil {
			return "", fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
		}
		return Encode(clampBytes(raw, rwiByteFlagLength)), nil
	}
	return value, nil
}

func parseRWIDecimalPrefix(value string) rwiDecimalPrefix {
	trimmed := strings.TrimLeft(value, " ")
	if trimmed == "" {
		return rwiDecimalPrefix{}
	}
	pos := 0
	negative := false
	if trimmed[pos] == '-' || trimmed[pos] == '+' {
		negative = trimmed[pos] == '-'
		pos++
		if pos == len(trimmed) {
			return rwiDecimalPrefix{}
		}
	}
	start := pos
	for pos < len(trimmed) && trimmed[pos] >= '0' && trimmed[pos] <= '9' {
		pos++
	}
	if pos == start {
		return rwiDecimalPrefix{}
	}
	magnitude, err := strconv.ParseUint(trimmed[start:pos], 10, 64)
	if err != nil {
		return rwiDecimalPrefix{}
	}
	return rwiDecimalPrefix{magnitude: magnitude, negative: negative}
}

func (n rwiDecimalPrefix) unsigned() uint64 {
	if !n.negative {
		return n.magnitude
	}
	return ^n.magnitude + 1
}

func fixedWidthUnsigned(n rwiDecimalPrefix, width int) uint64 {
	u := n.unsigned()
	if width >= 8 {
		return u
	}
	mask := uint64(1)<<(width*8) - 1
	return u & mask
}

func lowByte(n uint64) byte {
	return byte(n % 256)
}

func clampStringBytes(value string, width int) string {
	if len(value) <= width {
		return value
	}
	return value[:width]
}

func clampBytes(raw []byte, width int) []byte {
	if len(raw) <= width {
		return raw
	}
	return raw[:width]
}
