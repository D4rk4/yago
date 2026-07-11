package pageparse

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestReadSafetyLabelsRecognizesBoundedStructuredEvidence(t *testing.T) {
	longRating := strings.Repeat("x", maximumRatingRunes+1)
	page := ParseHTML(
		"https://example.org/",
		"text/html",
		[]byte(`<html><head>
<meta name="rating" content=" adult ">
<meta property="RATING" content="ADULT">
<meta http-equiv="rating" content="`+longRating+`">
<script type="application/ld+json; charset=utf-8">
{"@graph":[{"name":"page"},{"isFamilyFriendly":false}]}
</script>
</head></html>`),
	)
	if len(page.SafetyLabels.RatingValues) != 2 ||
		page.SafetyLabels.RatingValues[0] != "adult" ||
		len([]rune(page.SafetyLabels.RatingValues[1])) != maximumRatingRunes ||
		page.SafetyLabels.FamilyFriendly == nil || *page.SafetyLabels.FamilyFriendly {
		t.Fatalf("safety labels = %#v", page.SafetyLabels)
	}
}

func TestReadSafetyLabelsSkipsMalformedAndNonBooleanJSON(t *testing.T) {
	page := ParseHTML(
		"https://example.org/",
		"text/html",
		[]byte(`<html><head>
<meta name="other" content="adult">
<meta name="rating" content=" ">
<script type="text/plain">{"isFamilyFriendly":false}</script>
<script type="application/ld+json">{</script>
<script type="application/ld+json">{"isFamilyFriendly":"false","nested":{"value":1}}</script>
</head></html>`),
	)
	if len(page.SafetyLabels.RatingValues) != 0 || page.SafetyLabels.FamilyFriendly != nil {
		t.Fatalf("safety labels = %#v", page.SafetyLabels)
	}
}

func TestFamilyFriendlyFromJSONTraversesArraysAndObjects(t *testing.T) {
	value := []any{
		"ignored",
		map[string]any{"z": map[string]any{"ISFAMILYFRIENDLY": true}},
	}
	friendly, found := familyFriendlyFromJSON(value)
	if !found || !friendly {
		t.Fatalf("family friendly = %v/%v", friendly, found)
	}
	if friendly, found := familyFriendlyFromJSON(42); found || friendly {
		t.Fatalf("scalar family friendly = %v/%v", friendly, found)
	}
	if friendly, found := familyFriendlyFromJSON([]any{"ignored", 42}); found || friendly {
		t.Fatalf("array family friendly = %v/%v", friendly, found)
	}
}

func TestRatingValuesBoundCardinalityAndAttributeFallback(t *testing.T) {
	var fixture strings.Builder
	fixture.WriteString("<html><head>")
	for index := 0; index < maximumRatingValues+1; index++ {
		fixture.WriteString(`<meta property="rating" content="value`)
		fixture.WriteRune(rune('a' + index))
		fixture.WriteString(`">`)
	}
	fixture.WriteString("</head></html>")
	root, err := html.Parse(strings.NewReader(fixture.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	values := ratingValues(root)
	if len(values) != maximumRatingValues {
		t.Fatalf("rating values = %#v", values)
	}
	if got := firstNonEmptyAttribute(root, "missing"); got != "" {
		t.Fatalf("missing attribute = %q", got)
	}
}
