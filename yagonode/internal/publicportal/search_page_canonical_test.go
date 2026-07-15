package publicportal

import "testing"

func TestCanonicalPortalPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested int
		shown     int
		total     int
		page      int
		redirect  bool
	}{
		{name: "first", requested: 1, total: 100, page: 1},
		{name: "no results", requested: 2, total: 0, page: 1, redirect: true},
		{name: "past last", requested: 50, total: 45, page: 5, redirect: true},
		{name: "empty window", requested: 2, total: 100, page: 1, redirect: true},
		{name: "populated window", requested: 2, shown: 1, total: 100, page: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			page, redirect := canonicalPortalPage(
				test.requested,
				test.shown,
				test.total,
			)
			if page != test.page || redirect != test.redirect {
				t.Fatalf(
					"canonicalPortalPage(%d, %d, %d) = (%d, %t), want (%d, %t)",
					test.requested,
					test.shown,
					test.total,
					page,
					redirect,
					test.page,
					test.redirect,
				)
			}
		})
	}
}
