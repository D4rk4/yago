package pageparse_test

import (
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

func robotsHTML(head string) []byte {
	return fmt.Appendf(nil,
		`<html><head>%s</head><body><a href="/next">go</a> words</body></html>`,
		head,
	)
}

func TestParseHTMLReadsMetaRobots(t *testing.T) {
	cases := []struct {
		name         string
		head         string
		wantNoindex  bool
		wantNofollow bool
	}{
		{"noindex", `<meta name="robots" content="noindex">`, true, false},
		{"nofollow uppercase", `<meta name="robots" content="NOFOLLOW">`, false, true},
		{"none means both", `<meta name="robots" content="none">`, true, true},
		{"comma separated", `<meta name="robots" content="noindex,nofollow">`, true, true},
		{"absent", ``, false, false},
		{"googlebot name ignored", `<meta name="googlebot" content="noindex">`, false, false},
		{
			"description content ignored",
			`<meta name="description" content="how to noindex a page">`,
			false,
			false,
		},
		{"name case-insensitive", `<meta name="ROBOTS" content="noindex">`, true, false},
		{
			"multiple tags combine",
			`<meta name="robots" content="noindex"><meta name="robots" content="nofollow">`,
			true,
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := pageparse.ParseHTML(
				"https://example.com/",
				"text/html",
				robotsHTML(tc.head),
			)
			if page.MetaNoindex != tc.wantNoindex {
				t.Errorf("MetaNoindex = %v, want %v", page.MetaNoindex, tc.wantNoindex)
			}
			if page.MetaNofollow != tc.wantNofollow {
				t.Errorf("MetaNofollow = %v, want %v", page.MetaNofollow, tc.wantNofollow)
			}
		})
	}
}

func TestRobotsDirectivesHeaderForms(t *testing.T) {
	cases := []struct {
		value        string
		wantNoindex  bool
		wantNofollow bool
	}{
		{"noindex", true, false},
		{"nofollow", false, true},
		{"NOINDEX, NOFOLLOW", true, true},
		{"none", true, true},
		{"googlebot: noindex", true, false},
		{"unavailable_after: 25 Jun 2026 15:00:00 PST", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		noindex, nofollow := pageparse.RobotsDirectives(tc.value)
		if noindex != tc.wantNoindex || nofollow != tc.wantNofollow {
			t.Errorf("RobotsDirectives(%q) = %v/%v, want %v/%v",
				tc.value, noindex, nofollow, tc.wantNoindex, tc.wantNofollow)
		}
	}
}
