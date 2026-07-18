package yagoproto

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

func parseHashField(scope, field, raw string) (yagomodel.Hash, error) {
	if raw == "" {
		return "", nil
	}

	hash, err := yagomodel.ParseHash(raw)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", scope, field, err)
	}

	return hash, nil
}

func joinHashes(hashes []yagomodel.Hash) string {
	if len(hashes) == 0 {
		return ""
	}

	parts := make([]string, len(hashes))
	for i, h := range hashes {
		parts[i] = h.String()
	}

	return strings.Join(parts, ",")
}

func splitHashes(scope, field, raw string) ([]yagomodel.Hash, error) {
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	hashes := make([]yagomodel.Hash, 0, len(parts))
	for _, part := range parts {
		hash, err := parseHashField(scope, field, part)
		if err != nil {
			return nil, err
		}

		hashes = append(hashes, hash)
	}

	return hashes, nil
}

func concatHashes(hashes []yagomodel.Hash) string {
	if len(hashes) == 0 {
		return ""
	}

	var b strings.Builder
	for _, h := range hashes {
		b.WriteString(h.String())
	}

	return b.String()
}

func splitSearchHashes(field, raw string) ([]yagomodel.Hash, error) {
	limit := MaximumSearchTermHashes
	if field == FieldURLs {
		limit = MaximumSearchURLHashes
	}
	count := len(raw) / yagomodel.HashLength
	if count > limit {
		return nil, fmt.Errorf(
			"%w: %s contains %d hashes, maximum %d",
			ErrBadField,
			field,
			count,
			limit,
		)
	}
	hashes := make([]yagomodel.Hash, 0, count)
	for i := 0; i+yagomodel.HashLength <= len(raw); i += yagomodel.HashLength {
		hash, err := parseHashField("search request", field, raw[i:i+yagomodel.HashLength])
		if err != nil {
			return nil, err
		}

		hashes = append(hashes, hash)
	}

	return hashes, nil
}
