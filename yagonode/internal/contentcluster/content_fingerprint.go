package contentcluster

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const bandCount = 8

type preparedEvidence struct {
	URL                string
	ContentHash        string
	Fingerprint        uint64
	Shingles           []uint64
	Bands              [bandCount]uint8
	CanonicalPreferred bool
	Quality            float64
	InboundAuthority   float64
}

func prepareEvidence(
	ctx context.Context,
	limits Limits,
	evidence Evidence,
) (preparedEvidence, error) {
	if err := ctx.Err(); err != nil {
		return preparedEvidence{}, fmt.Errorf("check fingerprint context: %w", err)
	}
	url, err := validateURL(evidence.URL)
	if err != nil {
		return preparedEvidence{}, err
	}
	contentHash := strings.TrimSpace(evidence.ContentHash)
	if contentHash == "" || len(contentHash) > maximumContentHashBytes {
		return preparedEvidence{}, ErrInvalidEvidence
	}
	if math.IsNaN(evidence.Quality) || math.IsInf(evidence.Quality, 0) ||
		math.IsNaN(evidence.InboundAuthority) || math.IsInf(evidence.InboundAuthority, 0) {
		return preparedEvidence{}, ErrInvalidEvidence
	}
	quality := evidence.Quality
	if quality == 0 {
		quality = 0
	}
	authority := evidence.InboundAuthority
	if authority == 0 {
		authority = 0
	}
	words, err := normalizedWords(ctx, evidence.Text, limits)
	if err != nil {
		return preparedEvidence{}, err
	}
	shingles, fingerprint, err := fingerprintShingles(ctx, words, limits)
	if err != nil {
		return preparedEvidence{}, err
	}

	return preparedEvidence{
		URL:                url,
		ContentHash:        contentHash,
		Fingerprint:        fingerprint,
		Shingles:           shingles,
		Bands:              fingerprintBands(fingerprint),
		CanonicalPreferred: evidence.CanonicalPreferred,
		Quality:            quality,
		InboundAuthority:   authority,
	}, nil
}

func normalizedWords(ctx context.Context, text string, limits Limits) ([]string, error) {
	text = boundedValidText(text, limits.MaximumTextBytes)
	maximumWords := limits.MaximumShingles + limits.ShingleWords - 1
	words := make([]string, 0, min(maximumWords, 256))
	var word strings.Builder
	flush := func() bool {
		if word.Len() == 0 {
			return false
		}
		words = append(words, word.String())
		word.Reset()

		return len(words) == maximumWords
	}
	for _, current := range text {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("check word normalization context: %w", err)
		}
		if unicode.IsLetter(current) || unicode.IsNumber(current) ||
			(unicode.IsMark(current) && word.Len() > 0) {
			word.WriteRune(unicode.ToLower(current))
			continue
		}
		if flush() {
			return words, nil
		}
	}
	flush()

	return words, nil
}

func boundedValidText(text string, maximumBytes int) string {
	if len(text) > maximumBytes {
		cut := maximumBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}

	return strings.ToValidUTF8(text, " ")
}

func fingerprintShingles(
	ctx context.Context,
	words []string,
	limits Limits,
) ([]uint64, uint64, error) {
	if len(words) < limits.ShingleWords {
		return nil, 0, nil
	}
	unique := make(map[uint64]struct{}, min(limits.MaximumShingles, len(words)))
	for start := 0; start+limits.ShingleWords <= len(words); start++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("check shingle context: %w", err)
		}
		shingle := hashWords(words[start : start+limits.ShingleWords])
		unique[shingle] = struct{}{}
		if len(unique) == limits.MaximumShingles {
			break
		}
	}
	shingles := make([]uint64, 0, len(unique))
	for shingle := range unique {
		shingles = append(shingles, shingle)
	}
	sort.Slice(shingles, func(left, right int) bool {
		return shingles[left] < shingles[right]
	})
	var weights [64]int
	for _, shingle := range shingles {
		for bit := range weights {
			if shingle&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
	}
	var fingerprint uint64
	for bit, weight := range weights {
		if weight > 0 {
			fingerprint |= uint64(1) << bit
		}
	}

	return shingles, fingerprint, nil
}

func hashWords(words []string) uint64 {
	hasher := fnv.New64a()
	for position, word := range words {
		if position > 0 {
			_, _ = hasher.Write([]byte{0})
		}
		_, _ = hasher.Write([]byte(word))
	}

	return hasher.Sum64()
}

func fingerprintBands(fingerprint uint64) [bandCount]uint8 {
	var bands [bandCount]uint8
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], fingerprint)
	copy(bands[:], encoded[:])

	return bands
}

func boundedJaccard(left, right []uint64) float64 {
	leftPosition := 0
	rightPosition := 0
	intersection := 0
	for leftPosition < len(left) && rightPosition < len(right) {
		switch {
		case left[leftPosition] == right[rightPosition]:
			intersection++
			leftPosition++
			rightPosition++
		case left[leftPosition] < right[rightPosition]:
			leftPosition++
		default:
			rightPosition++
		}
	}
	union := len(left) + len(right) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}
