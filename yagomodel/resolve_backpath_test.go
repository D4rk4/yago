package yagomodel

import "testing"

func TestResolveBackpath(t *testing.T) {
	cases := map[string]string{
		"/a/b/../c":    "/a/c",
		"/a/b/..":      "/a",
		"/a/./b":       "/a/b",
		"/a//b":        "/a/b",
		"/a/b/../../c": "/c",
		"/../a":        "/a",
		"/..":          "/",
		"":             "/",
		"/a/b/c":       "/a/b/c",
		"a/b":          "/a/b",
	}
	for in, want := range cases {
		if got := resolveBackpath(in); got != want {
			t.Errorf("resolveBackpath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURLHashNormalizesDotSegments(t *testing.T) {
	a, err := HashURL("http://example.com/a/b/../c")
	if err != nil {
		t.Fatalf("HashURL a: %v", err)
	}
	b, err := HashURL("http://example.com/a/c")
	if err != nil {
		t.Fatalf("HashURL b: %v", err)
	}
	if a != b {
		t.Errorf("dot-segment url hash %q must equal normalized %q", a, b)
	}
}

func TestResolveBackpathLeavesQueryUntouched(t *testing.T) {
	got := parseURLAddress("http://example.com/a/b/../c?x=/../y").normalform()
	want := "http://example.com/a/c?x=/../y"
	if got != want {
		t.Errorf("normalform = %q, want %q", got, want)
	}
}
