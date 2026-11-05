package pageindex

import "testing"

func TestURLComponentCountMatchesJavaGoldenValues(t *testing.T) {
	cases := map[string]int{
		"https://example.com/article":                3,
		"https://www.example.org/terms-conditions/":  5,
		"https://example.com/a//b":                   5,
		"http://":                                    1,
		"http:///":                                   0,
		"https://example.com/page?query=hello+world": 4,
		"https://example.com:8080/path/a-b_c.d,e":    9,
		"http://[::1]:80/path/x//y":                  8,
		"http://example.com/a-b_c.d,e?f=g!":          8,
		"example.com/article":                        3,
		"www.example.com/page":                       4,
		"http://www.例子.test/x":                       4,
		"http://www.例子.test/路径/x":                    5,
		"https://x.com/中文/path":                      4,
		"https://e/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa": 9,
		"":                           1,
		"   ":                        0,
		"http://a":                   1,
		"http://example.com/a b c":   5,
		"http://example.com/(a)/(b)": 7,
		"http://example.com/a'b":     4,
		`http://example.com/a"b`:     4,
	}
	for raw, want := range cases {
		if got := urlComponentCount(raw); got != want {
			t.Errorf("urlComponentCount(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestURLComponentCountBeyond255(t *testing.T) {
	var raw string
	for i := 0; i < 300; i++ {
		raw += "aaaaaaaaaa/"
	}
	if got := urlComponentCount(raw); got != 300 {
		t.Fatalf("urlComponentCount(300 comps) = %d, want 300", got)
	}
}

func TestURLComponentCountTrailingEmptiesDropped(t *testing.T) {
	if got := urlComponentCount("http://example.com/a/b/"); got != 4 {
		t.Fatalf("trailing slash count = %d, want 4", got)
	}
	if got := urlComponentCount("http://example.com/a//"); got != 3 {
		t.Fatalf("trailing double slash count = %d, want 3", got)
	}
	if got := urlComponentCount("http://example.com/a/b/c/d/e"); got != 7 {
		t.Fatalf("multi-segment count = %d, want 7", got)
	}
}
