package modifierhint

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		req   searchcore.Request
		total int
		want  bool
	}{
		{"filetype only, zero", searchcore.Request{FileType: "pdf"}, 0, true},
		{"site only, zero", searchcore.Request{SiteHost: "example.com"}, 0, true},
		{"tld only, zero", searchcore.Request{TLD: "org"}, 0, true},
		{"inurl only, zero", searchcore.Request{InURL: "docs"}, 0, true},
		{"filter with results", searchcore.Request{FileType: "pdf"}, 5, false},
		{
			"filter with terms",
			searchcore.Request{FileType: "pdf", Terms: []string{"go"}},
			0,
			false,
		},
		{"no filter, zero", searchcore.Request{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Text(tc.req, tc.total)
			if (got != "") != tc.want {
				t.Fatalf("Text(%+v, %d) = %q, want non-empty=%v", tc.req, tc.total, got, tc.want)
			}
		})
	}
}
