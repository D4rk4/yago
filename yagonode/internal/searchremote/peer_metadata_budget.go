package searchremote

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	remoteMetadataURLByteLimit      = 8 << 10
	remoteMetadataTitleByteLimit    = 2 << 10
	remoteMetadataLanguageByteLimit = 64
)

var errRemoteSearchDecodedBudgetExhausted = errors.New(
	"remote search decoded metadata budget exhausted",
)

func decodedMetadataPropertyWithinBudget(
	ctx context.Context,
	row yagomodel.URIMetadataRow,
	key string,
	maximumBytes int,
	budget *remoteQueryBudget,
) (string, error) {
	raw := row.Properties[key]
	if raw == "" {
		return "", nil
	}
	value, err := yagomodel.DecodeWireFormWithLimit(ctx, raw, int64(maximumBytes))
	if err != nil {
		return "", fmt.Errorf("decode url metadata %s: %w", key, err)
	}
	if err := budget.retainDecodedMetadata(len(value)); err != nil {
		return "", err
	}

	return value, nil
}

func boundedRowLanguage(
	row yagomodel.URIMetadataRow,
	budget *remoteQueryBudget,
) (string, error) {
	raw := strings.TrimSpace(row.Properties["lang"])
	if len(raw) > remoteMetadataLanguageByteLimit {
		return "", fmt.Errorf(
			"%w: language exceeds %d bytes",
			errRemoteSearchInvalidResult,
			remoteMetadataLanguageByteLimit,
		)
	}
	language := strings.ToLower(raw)
	if err := budget.retainDecodedMetadata(len(language)); err != nil {
		return "", err
	}

	return language, nil
}

func (budget *remoteQueryBudget) retainDecodedMetadata(retained int) error {
	if retained > budget.decodedBytesRemaining {
		return fmt.Errorf(
			"%w: maximum %d bytes",
			errRemoteSearchDecodedBudgetExhausted,
			remoteQueryDecodedByteBudget,
		)
	}
	budget.decodedBytesRemaining -= retained

	return nil
}
