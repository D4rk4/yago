package yagonode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/extractfetch"
)

func parseExtractFetchResponseBytes(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 1 || value > extractfetch.MaximumResponseBytes {
		return 0, fmt.Errorf(
			"extract response limit must be between 1 and %d bytes",
			extractfetch.MaximumResponseBytes,
		)
	}

	return value, nil
}

func normalizeExtractFetchResponseBytes(raw string) (string, error) {
	value, err := parseExtractFetchResponseBytes(raw)
	if err != nil {
		return "", err
	}

	return strconv.FormatInt(value, 10), nil
}
