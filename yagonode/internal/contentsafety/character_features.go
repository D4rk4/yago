package contentsafety

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
)

const (
	CharacterFeatureSpaceSize       = 4096
	MaximumLabeledDocuments         = 256
	MaximumLabeledDocumentRunes     = 8192
	minimumCharacterGramLength      = 3
	maximumCharacterGramLength      = 5
	characterHashOffsetBasis        = uint64(14695981039346656037)
	characterHashPrime              = uint64(1099511628211)
	characterHashAvalancheFactorOne = uint64(0xff51afd7ed558ccd)
	characterHashAvalancheFactorTwo = uint64(0xc4ceb9fe1a85ec53)
)

type characterFeature struct {
	position int
	value    float64
}

func characterFeatures(text string) []characterFeature {
	features, _ := characterFeaturesWithContext(context.Background(), text)

	return features
}

func characterFeaturesWithContext(
	ctx context.Context,
	text string,
) ([]characterFeature, error) {
	characters := normalizedCharacters(text)
	if len(characters) < minimumCharacterGramLength {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extract character features: %w", err)
		}

		return nil, nil
	}
	values := make(map[int]float64, CharacterFeatureSpaceSize)
	for start := range characters {
		if start%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("extract character features: %w", err)
			}
		}
		for length := minimumCharacterGramLength; length <= maximumCharacterGramLength; length++ {
			if start+length > len(characters) {
				break
			}
			position, sign := signedCharacterBucket(characters[start : start+length])
			values[position] += sign
		}
	}

	return normalizedCharacterFeatures(values), nil
}

func normalizedCharacters(text string) []rune {
	characters := make([]rune, 0, min(len(text), MaximumLabeledDocumentRunes))
	spacePending := false
	consumed := 0
	for _, character := range text {
		if consumed == MaximumLabeledDocumentRunes {
			break
		}
		consumed++
		if unicode.IsSpace(character) {
			spacePending = len(characters) > 0
			continue
		}
		if spacePending {
			characters = append(characters, ' ')
			spacePending = false
		}
		characters = append(characters, unicode.ToLower(character))
	}

	return characters
}

func signedCharacterBucket(characters []rune) (int, float64) {
	value := characterHashOffsetBasis
	for _, character := range characters {
		for _, octet := range []byte(string(character)) {
			value ^= uint64(octet)
			value *= characterHashPrime
		}
	}
	value ^= value >> 33
	value *= characterHashAvalancheFactorOne
	value ^= value >> 33
	value *= characterHashAvalancheFactorTwo
	value ^= value >> 33
	position := int(value & (CharacterFeatureSpaceSize - 1))
	if value&(uint64(1)<<63) != 0 {
		return position, -1
	}

	return position, 1
}

func normalizedCharacterFeatures(values map[int]float64) []characterFeature {
	squaredLength := 0.0
	for _, value := range values {
		squaredLength += value * value
	}
	if squaredLength == 0 {
		return nil
	}
	length := math.Sqrt(squaredLength)
	features := make([]characterFeature, 0, len(values))
	for position, value := range values {
		if value != 0 {
			features = append(features, characterFeature{position: position, value: value / length})
		}
	}
	slices.SortFunc(features, func(left, right characterFeature) int {
		return left.position - right.position
	})

	return features
}

func boundedDocument(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	consumed := 0
	for range text {
		consumed++
		if consumed > MaximumLabeledDocumentRunes {
			return false
		}
	}

	return true
}
