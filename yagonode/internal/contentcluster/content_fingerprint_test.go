package contentcluster

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestUnicodeWordShingleFingerprintIsDeterministicAndBounded(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumTextBytes = 256
	limits.MaximumShingles = 3
	limits.ShingleWords = 2
	first, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         " https://a.example ",
		ContentHash: " hash ",
		Text:        "ÉCOLE, ΔΈΛΤΑ 42 beta gamma delta epsilon zeta",
		Quality:     math.Copysign(0, -1),
	})
	if err != nil {
		t.Fatalf("prepare first: %v", err)
	}
	second, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://a.example",
		ContentHash: "hash",
		Text:        "école δέλτα 42 BETA gamma delta epsilon zeta",
	})
	if err != nil {
		t.Fatalf("prepare second: %v", err)
	}
	if first.URL != "https://a.example" || first.ContentHash != "hash" {
		t.Fatalf("trimmed evidence = %+v", first)
	}
	if first.Quality != 0 || math.Signbit(first.Quality) {
		t.Fatalf("normalized zero = %v", first.Quality)
	}
	if !reflect.DeepEqual(first.Shingles, second.Shingles) ||
		first.Fingerprint != second.Fingerprint {
		t.Fatalf("normalized fingerprints differ: %+v %+v", first, second)
	}
	if len(first.Shingles) != limits.MaximumShingles {
		t.Fatalf("shingles = %d, want %d", len(first.Shingles), limits.MaximumShingles)
	}
	for iteration := 0; iteration < 100; iteration++ {
		again, repeatErr := prepareEvidence(t.Context(), limits, Evidence{
			URL:         "https://a.example",
			ContentHash: "hash",
			Text:        "école δέλτα 42 beta gamma delta epsilon zeta",
		})
		if repeatErr != nil || again.Fingerprint != first.Fingerprint ||
			!reflect.DeepEqual(again.Shingles, first.Shingles) {
			t.Fatalf("iteration %d fingerprint = %+v, %v", iteration, again, repeatErr)
		}
	}
}

func TestTextAndWordBoundsHandleBrokenUnicode(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumTextBytes = 6
	limits.MaximumShingles = 2
	limits.ShingleWords = 1
	words, err := normalizedWords(t.Context(), "ab écd extra\xffignored", limits)
	if err != nil {
		t.Fatalf("normalize bounded text: %v", err)
	}
	if !reflect.DeepEqual(words, []string{"ab", "éc"}) {
		t.Fatalf("bounded words = %q", words)
	}
	if got := boundedValidText("éclair", 1); got != "" {
		t.Fatalf("partial leading rune = %q", got)
	}
	if got := boundedValidText("ok", 2); got != "ok" {
		t.Fatalf("unbounded text = %q", got)
	}
	words, err = normalizedWords(t.Context(), "\u0301 alpha \u0301beta", Limits{
		MaximumTextBytes:  64,
		MaximumShingles:   8,
		MaximumCandidates: 1,
		ShingleWords:      1,
	})
	if err != nil {
		t.Fatalf("normalize marks: %v", err)
	}
	if !reflect.DeepEqual(words, []string{"alpha", "beta"}) {
		t.Fatalf("mark words = %q", words)
	}
}

func TestShingleFingerprintAndJaccardEdges(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 2
	if shingles, fingerprint, err := fingerprintShingles(
		t.Context(),
		[]string{"one"},
		limits,
	); err != nil || shingles != nil ||
		fingerprint != 0 {
		t.Fatalf("short fingerprint = %v, %d, %v", shingles, fingerprint, err)
	}
	words := []string{"one", "two", "three", "four"}
	shingles, fingerprint, err := fingerprintShingles(t.Context(), words, limits)
	if err != nil || len(shingles) != 3 || fingerprint == 0 {
		t.Fatalf("fingerprint = %v, %d, %v", shingles, fingerprint, err)
	}
	if got := boundedJaccard(nil, nil); got != 0 {
		t.Fatalf("empty Jaccard = %v", got)
	}
	if got := boundedJaccard([]uint64{1, 2, 4}, []uint64{2, 3, 4}); got != 0.5 {
		t.Fatalf("mixed Jaccard = %v", got)
	}
	if got := boundedJaccard([]uint64{1}, []uint64{2, 3}); got != 0 {
		t.Fatalf("disjoint Jaccard = %v", got)
	}
	bands := fingerprintBands(fingerprint)
	if len(bands) != bandCount || bandKey(3, bands[3])[0] != 3 {
		t.Fatalf("bands = %v", bands)
	}
	if hashWords([]string{"one", "two"}) == hashWords([]string{"onetwo"}) {
		t.Fatal("word boundary did not affect shingle hash")
	}
}

func TestInternalCancellationPaths(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 1
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := normalizedWords(cancelled, "one two", limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("normalization cancellation = %v", err)
	}
	if _, _, err := fingerprintShingles(
		cancelled,
		[]string{"one", "two"},
		limits,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("shingle cancellation = %v", err)
	}
	if _, err := prepareEvidence(
		cancelled,
		limits,
		Evidence{URL: "https://a.example", ContentHash: "hash"},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("preparation cancellation = %v", err)
	}
}

func TestLimitsDefaultsAndValidation(t *testing.T) {
	defaults, err := completeLimits(Limits{})
	if err != nil || defaults != DefaultLimits() {
		t.Fatalf("default limits = %+v, %v", defaults, err)
	}
	valid := DefaultLimits()
	if completed, validErr := completeLimits(valid); validErr != nil || completed != valid {
		t.Fatalf("explicit limits = %+v, %v", completed, validErr)
	}
	invalid := []Limits{
		{MaximumTextBytes: -1},
		{MaximumTextBytes: hardMaximumTextBytes + 1},
		{MaximumShingles: -1},
		{MaximumShingles: hardMaximumShingles + 1},
		{MaximumCandidates: -1},
		{MaximumCandidates: hardMaximumCandidates + 1},
		{MaximumBucketMembers: -1},
		{MaximumBucketMembers: hardMaximumBucketMembers + 1},
		{MaximumClusterMembers: -1},
		{MaximumClusterMembers: hardMaximumClusterMembers + 1},
		{ShingleWords: -1},
		{ShingleWords: hardMaximumShingleWords + 1},
		{MinimumJaccard: math.NaN()},
		{MinimumJaccard: -0.1},
		{MinimumJaccard: 1.1},
	}
	for position, limits := range invalid {
		if _, invalidErr := completeLimits(limits); invalidErr == nil {
			t.Fatalf("invalid limits %d accepted: %+v", position, limits)
		}
	}
	if _, err := validateURL(
		strings.Repeat("x", maximumURLBytes+1),
	); !errors.Is(
		err,
		ErrInvalidEvidence,
	) {
		t.Fatalf("long URL error = %v", err)
	}
}

func TestJSONCodecRoundTripAndErrors(t *testing.T) {
	codec := jsonCodec[fingerprintRecord]{}
	want := fingerprintRecord{URL: "https://a.example", Shingles: []uint64{1, 2}, Quality: 0.5}
	raw, err := codec.Encode(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := codec.Decode(raw)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("decode = %+v, %v", got, err)
	}
	if _, err := codec.Decode([]byte("{")); err == nil {
		t.Fatal("invalid JSON decoded")
	}
	if _, err := (jsonCodec[float64]{}).Encode(math.NaN()); err == nil {
		t.Fatal("non-JSON number encoded")
	}
}
