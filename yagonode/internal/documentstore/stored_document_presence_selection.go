package documentstore

import "github.com/D4rk4/yago/yagonode/internal/vault"

type storedDocumentPresenceSelection struct {
	orderedKeys      []vault.Key
	orderedPositions []int
	legacyKeys       []vault.Key
	legacyPositions  []int
}

func documentPresenceKeys(normalizedURLs []string) []vault.Key {
	keys := make([]vault.Key, len(normalizedURLs))
	for index, normalizedURL := range normalizedURLs {
		keys[index] = vault.Key(normalizedURL)
	}

	return keys
}

func selectStoredDocumentPresence(
	normalizedURLs []string,
	keys []vault.Key,
	admissions []uint64,
	located []bool,
) (storedDocumentPresenceSelection, error) {
	selection := storedDocumentPresenceSelection{
		orderedKeys:      make([]vault.Key, 0, len(keys)),
		orderedPositions: make([]int, 0, len(keys)),
		legacyKeys:       make([]vault.Key, 0, len(keys)),
		legacyPositions:  make([]int, 0, len(keys)),
	}
	for index, key := range keys {
		if !located[index] {
			selection.legacyKeys = append(selection.legacyKeys, key)
			selection.legacyPositions = append(selection.legacyPositions, index)

			continue
		}
		orderedKey, err := orderedDocumentKey(admissions[index], normalizedURLs[index])
		if err != nil {
			return storedDocumentPresenceSelection{}, err
		}
		selection.orderedKeys = append(selection.orderedKeys, orderedKey)
		selection.orderedPositions = append(selection.orderedPositions, index)
	}

	return selection, nil
}

func (s storedDocumentPresenceSelection) record(
	found []bool,
	ordered []bool,
	legacy []bool,
) {
	for index, position := range s.orderedPositions {
		found[position] = ordered[index]
	}
	for index, position := range s.legacyPositions {
		found[position] = legacy[index]
	}
}
