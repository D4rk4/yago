package publicportal

import (
	"strings"
	"testing"
)

func TestPortalRendersAccessibleAutocomplete(t *testing.T) {
	_, body := get(t, New(&fakeSource{}, false), "/")
	for _, want := range []string{
		`role="combobox"`,
		`aria-autocomplete="list"`,
		`aria-expanded="false"`,
		`aria-controls="ac-list"`,
		`role="listbox"`,
		`autocomplete="off"`,
		"/suggest.json?q=",
		"aria-activedescendant",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal page missing %q", want)
		}
	}
}

func TestPortalSearchFieldKeepsItsWidth(t *testing.T) {
	// Regression: the autocomplete span wraps the input, so the input's width must
	// live on the span (a bare percentage width on the input collapses against the
	// shrink-to-fit inline-block and squeezes the field to a sliver).
	_, body := get(t, New(&fakeSource{}, false), "/")
	for _, want := range []string{
		".ac-wrap { position: relative; display: inline-block; width: 70%; max-width: 32rem;",
		".search input[type=search] { width: 100%;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal search field styling missing %q", want)
		}
	}
}

func TestPortalRendersOperatorHelp(t *testing.T) {
	_, body := get(t, New(&fakeSource{}, false), "/")
	for _, want := range []string{
		"Search operators", "site:example.org", "filetype:pdf", "/date", "-word",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal help missing %q", want)
		}
	}
}
