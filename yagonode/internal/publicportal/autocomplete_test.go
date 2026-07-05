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
