package yagonode

import (
	"strings"
	"testing"
)

// The portal name is bounded in characters, not bytes, because the bound exists
// to keep the header on one line. Only the rejecting side of that bound is
// pinned elsewhere, so a bound counted in bytes would pass unnoticed while
// silently refusing every non-Latin portal name of legitimate length. This pins
// the accepting side at exactly the limit, in one-byte and multi-byte
// characters alike, and the first character past it.
func TestPortalGreetingBoundCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()

	normalize := indexSettingDefinitions()["portal.greeting"].normalize
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "at the limit", value: strings.Repeat("x", 60)},
		{name: "at the limit in two-byte characters", value: strings.Repeat("é", 60)},
		{name: "at the limit in four-byte characters", value: strings.Repeat("𝕐", 60)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalize(test.value)
			if err != nil || got != test.value {
				t.Fatalf(
					"portal name of %d characters (%d bytes) = %q, %v",
					len([]rune(test.value)),
					len(test.value),
					got,
					err,
				)
			}
		})
	}
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "one character past the limit", value: strings.Repeat("x", 61)},
		{
			name:  "one two-byte character past the limit",
			value: strings.Repeat("é", 61),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := normalize(test.value); err == nil {
				t.Fatalf(
					"portal name of %d characters accepted",
					len([]rune(test.value)),
				)
			}
		})
	}
}
