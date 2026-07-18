package adminauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var (
	errInvalidAPIKeyPageCursor             = errors.New("invalid API key page cursor")
	errInvalidAPIKeyPageLimit              = errors.New("invalid API key page limit")
	errAPIKeyCompatibilityListingTruncated = errors.New("API key listing requires pagination")
)

type apiKeyListingPage struct {
	infos      []apiKeyInfo
	nextCursor string
	total      int
}

func (s *apiKeyStore) page(
	ctx context.Context,
	cursor string,
	limit int,
) (apiKeyListingPage, error) {
	if !validAPIKeyPageCursor(cursor) {
		return apiKeyListingPage{}, errInvalidAPIKeyPageCursor
	}
	if limit < 1 || limit > maximumAPIKeys {
		return apiKeyListingPage{}, errInvalidAPIKeyPageLimit
	}

	var result apiKeyListingPage
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		total, err := s.records.Len(tx)
		if err != nil {
			return fmt.Errorf("measure api key records: %w", err)
		}
		result.total = total
		var after vault.Key
		if cursor != "" {
			after = vault.Key(cursor)
		}
		page, err := tx.ReadBucketPage(adminAPIKeysBucket, after, limit)
		if err != nil {
			return fmt.Errorf("read api key page: %w", err)
		}
		infos, err := decodeAPIKeyPage(page.Entries)
		if err != nil {
			return err
		}
		if cursor == "" && !page.More {
			sortAPIKeyInfos(infos)
		}
		result.infos = infos
		if page.More && len(infos) > 0 {
			result.nextCursor = infos[len(infos)-1].ID
		}

		return nil
	}); err != nil {
		return apiKeyListingPage{}, fmt.Errorf("view api keys: %w", err)
	}

	return result, nil
}

func decodeAPIKeyPage(entries []vault.BucketPageEntry) ([]apiKeyInfo, error) {
	infos := make([]apiKeyInfo, 0, len(entries))
	codec := apiKeyRecordCodec{}
	for _, entry := range entries {
		id := string(entry.Key)
		if !validAPIKeyPageCursor(id) || id == "" {
			return nil, fmt.Errorf("decode api key page: invalid identifier")
		}
		record, err := codec.Decode(entry.Value)
		if err != nil {
			return nil, fmt.Errorf("decode api key page: %w", err)
		}
		infos = append(infos, infoFromRecord(id, record))
	}

	return infos, nil
}

func validAPIKeyPageCursor(cursor string) bool {
	if cursor == "" {
		return true
	}
	if len(cursor) != apiKeyIDLen {
		return false
	}
	var decoded [apiKeyIDBytes]byte
	length, err := base64.RawURLEncoding.Decode(decoded[:], []byte(cursor))

	return err == nil && length == apiKeyIDBytes
}

func ValidAPIKeyPageCursor(cursor string) bool {
	return validAPIKeyPageCursor(cursor)
}

func sortAPIKeyInfos(infos []apiKeyInfo) {
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CreatedAt.Equal(infos[j].CreatedAt) {
			return infos[i].ID < infos[j].ID
		}

		return infos[i].CreatedAt.Before(infos[j].CreatedAt)
	})
}
