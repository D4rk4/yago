package yacymodel

import "testing"

func TestFreshnessPrefersLoadThenModThenFresh(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]string
		want  string
	}{
		{"load wins", map[string]string{ColLoadDate: "1", ColModDate: "2", ColFreshDate: "3"}, "1"},
		{"mod fallback", map[string]string{ColModDate: "2", ColFreshDate: "3"}, "2"},
		{"fresh fallback", map[string]string{ColFreshDate: "3"}, "3"},
		{"none", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := URIMetadataRow{Properties: tc.props}.Freshness()
			if got != tc.want {
				t.Fatalf("Freshness() = %q, want %q", got, tc.want)
			}
		})
	}
}
