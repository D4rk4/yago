package peernews

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type knownCategoryCodec struct{}

const maximumKnownCategoryEvidenceBytes = categoryMaxLength + 1 + 16

func (knownCategoryCodec) Encode(category string) ([]byte, error) {
	if _, _, _, err := decodeKnownCategoryEvidence(category); err != nil {
		return nil, err
	}

	return []byte(category), nil
}

func (knownCategoryCodec) Decode(raw []byte) (string, error) {
	encoded := string(raw)
	if _, _, _, err := decodeKnownCategoryEvidence(encoded); err != nil {
		return "", err
	}

	return encoded, nil
}

func (p *Pool) knownNewsCategory(
	tx *vault.Txn,
	key vault.Key,
) (string, bool, error) {
	encoded, found, err := p.storedKnownCategoryEvidence(tx, key)
	if err != nil {
		return "", false, err
	}
	if !found {
		return knownMarker, false, nil
	}
	category, _, _, _ := decodeKnownCategoryEvidence(encoded)

	return category, true, nil
}

func (p *Pool) storedKnownCategoryEvidence(
	tx *vault.Txn,
	key vault.Key,
) (string, bool, error) {
	size, found, err := p.knownCategories.EncodedSize(tx, key)
	if err != nil {
		return "", found, fmt.Errorf("inspect known news category: %w", err)
	}
	if !found {
		return "", false, nil
	}
	if size > maximumKnownCategoryEvidenceBytes {
		return "", true, fmt.Errorf(
			"%w: %w: known news category size %d",
			vault.ErrCorruptValue,
			ErrBadNewsRecord,
			size,
		)
	}

	category, present, err := p.knownCategories.Get(tx, key)
	if err != nil {
		return "", true, fmt.Errorf("read known news category: %w", err)
	}
	if !present {
		return "", true, fmt.Errorf(
			"%w: known news category disappeared during read",
			vault.ErrCorruptValue,
		)
	}

	return category, true, nil
}

func decodeKnownCategoryEvidence(encoded string) (string, string, bool, error) {
	category, generation, bound := strings.Cut(encoded, "\x00")
	if category == "" || len(category) > categoryMaxLength {
		return "", "", false, fmt.Errorf(
			"%w: known news category %q", ErrBadNewsRecord, category,
		)
	}
	if bound {
		decoded, err := hex.DecodeString(generation)
		if err != nil || len(decoded) != 8 {
			return "", "", false, fmt.Errorf(
				"%w: known news generation %q", ErrBadNewsRecord, generation,
			)
		}
	}

	return category, generation, bound, nil
}

func knownCategoryGeneration(record Record) string {
	record.Distributed = 0
	sum := sha256.Sum256([]byte(record.WireForm()))

	return hex.EncodeToString(sum[:8])
}

func knownCategoryEvidence(record Record) string {
	return record.Category + "\x00" + knownCategoryGeneration(record)
}

func (p *Pool) replaceKnownNewsCategory(
	tx *vault.Txn,
	key vault.Key,
	category string,
) error {
	if category != "" {
		if err := p.knownCategories.Put(tx, key, category); err != nil {
			return fmt.Errorf("store known news category: %w", err)
		}

		return nil
	}
	if _, err := p.knownCategories.Delete(tx, key); err != nil {
		return fmt.Errorf("clear known news category: %w", err)
	}

	return nil
}

func (p *Pool) replaceKnownNewsCategoryForRecord(
	tx *vault.Txn,
	key vault.Key,
	record Record,
) error {
	if err := p.knownCategories.Put(tx, key, knownCategoryEvidence(record)); err != nil {
		return fmt.Errorf("store known news category: %w", err)
	}

	return nil
}

func (p *Pool) forgetKnownNews(tx *vault.Txn, key vault.Key) error {
	if _, err := p.knownCategories.Delete(tx, key); err != nil {
		return fmt.Errorf("forget known news category: %w", err)
	}
	if _, err := p.known.Delete(tx, key); err != nil {
		return fmt.Errorf("forget known news: %w", err)
	}

	return nil
}
