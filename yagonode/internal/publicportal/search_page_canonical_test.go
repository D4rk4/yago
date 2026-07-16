package publicportal

import "testing"

func TestCanonicalPortalPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested int
		shown     int
		available int
		exhausted bool
		page      int
		redirect  bool
	}{
		{name: "first", requested: 1, available: 100, exhausted: true, page: 1},
		{name: "complete no results", requested: 2, exhausted: true, page: 1, redirect: true},
		{name: "incomplete no results", requested: 2, page: 2},
		{
			name:      "past confirmed last",
			requested: 50,
			available: 45,
			exhausted: true,
			page:      5,
			redirect:  true,
		},
		{name: "empty confirmed window", requested: 2, available: 100, exhausted: true, page: 2},
		{name: "populated window", requested: 2, shown: 1, available: 11, exhausted: true, page: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			page, redirect := canonicalPortalPage(
				test.requested,
				test.shown,
				test.available,
				test.exhausted,
			)
			if page != test.page || redirect != test.redirect {
				t.Fatalf(
					"canonicalPortalPage(%d, %d, %d, %t) = (%d, %t), want (%d, %t)",
					test.requested,
					test.shown,
					test.available,
					test.exhausted,
					page,
					redirect,
					test.page,
					test.redirect,
				)
			}
		})
	}
}
