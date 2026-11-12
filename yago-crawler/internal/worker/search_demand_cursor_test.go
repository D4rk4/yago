package worker

import "testing"

func TestSearchDemandCursorRegressed(t *testing.T) {
	for _, test := range []struct {
		name    string
		current string
		next    string
		want    bool
	}{
		{name: "node feed was restored", current: "55", next: "6", want: true},
		{name: "cursor is unchanged", current: "6", next: "6"},
		{name: "cursor advances", current: "6", next: "7"},
		{name: "invalid current cursor", current: "invalid", next: "1"},
		{name: "invalid node cursor", current: "6", next: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := searchDemandCursorRegressed(test.current, test.next); got != test.want {
				t.Fatalf("searchDemandCursorRegressed(%q, %q) = %t, want %t",
					test.current, test.next, got, test.want)
			}
		})
	}
}
