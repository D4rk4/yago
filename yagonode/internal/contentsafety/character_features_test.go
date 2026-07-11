package contentsafety

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizedCharacters(t *testing.T) {
	if got, want := normalizedCharacters(
		"  Ä\t\nB  ",
	), []rune(
		"ä b",
	); !reflect.DeepEqual(
		got,
		want,
	) {
		t.Fatalf("normalizedCharacters() = %q, want %q", string(got), string(want))
	}
	bounded := normalizedCharacters(strings.Repeat("界", MaximumLabeledDocumentRunes+1))
	if len(bounded) != MaximumLabeledDocumentRunes {
		t.Fatalf("bounded character length = %d", len(bounded))
	}
}

func TestSignedCharacterBucketIsDeterministicAndSigned(t *testing.T) {
	position, sign := signedCharacterBucket([]rune("東京案"))
	repeatedPosition, repeatedSign := signedCharacterBucket([]rune("東京案"))
	if position != repeatedPosition || sign != repeatedSign ||
		position < 0 || position >= CharacterFeatureSpaceSize {
		t.Fatalf(
			"unstable signed bucket = %d, %v then %d, %v",
			position,
			sign,
			repeatedPosition,
			repeatedSign,
		)
	}
	positive := false
	negative := false
	for value := range 10000 {
		_, candidateSign := signedCharacterBucket([]rune(fmt.Sprintf("%05d", value)))
		positive = positive || candidateSign > 0
		negative = negative || candidateSign < 0
		if positive && negative {
			break
		}
	}
	if !positive || !negative {
		t.Fatalf("signed hashing signs: positive=%v negative=%v", positive, negative)
	}
}

func TestCharacterFeaturesNormalizeAndCancel(t *testing.T) {
	features := characterFeatures("Alpha   Βήτα 東京")
	if len(features) == 0 {
		t.Fatal("multilingual features are empty")
	}
	squaredLength := 0.0
	previousPosition := -1
	for _, feature := range features {
		squaredLength += feature.value * feature.value
		if feature.position <= previousPosition {
			t.Fatalf("features are not ordered: %#v", features)
		}
		previousPosition = feature.position
	}
	if math.Abs(squaredLength-1) > 1e-12 {
		t.Fatalf("feature squared length = %v", squaredLength)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := characterFeaturesWithContext(canceled, "a"); err == nil {
		t.Fatal("short canceled extraction succeeded")
	}
	if _, err := characterFeaturesWithContext(canceled, "long enough text"); err == nil {
		t.Fatal("canceled extraction succeeded")
	}
	if got, err := characterFeaturesWithContext(
		context.Background(),
		"ab",
	); err != nil ||
		got != nil {
		t.Fatalf("short extraction = %#v, %v", got, err)
	}
}

func TestNormalizedCharacterFeaturesHandlesCollisions(t *testing.T) {
	if got := normalizedCharacterFeatures(map[int]float64{1: 0}); got != nil {
		t.Fatalf("zero features = %#v", got)
	}
	features := normalizedCharacterFeatures(map[int]float64{3: 4, 1: 0, 2: 3})
	want := []characterFeature{{position: 2, value: 0.6}, {position: 3, value: 0.8}}
	if !reflect.DeepEqual(features, want) {
		t.Fatalf("normalized features = %#v, want %#v", features, want)
	}
}

func TestBoundedDocument(t *testing.T) {
	if boundedDocument(" \t\n ") {
		t.Fatal("blank document is bounded training data")
	}
	if !boundedDocument(strings.Repeat("a", MaximumLabeledDocumentRunes)) {
		t.Fatal("maximum-size document is rejected")
	}
	if boundedDocument(strings.Repeat("a", MaximumLabeledDocumentRunes+1)) {
		t.Fatal("oversize document is accepted")
	}
}
