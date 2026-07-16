package httproute

import "testing"

func TestCanonicalPath(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"":                                  "/",
		"/":                                 "/",
		".":                                 "/",
		"/../":                              "/",
		"admin/assets/carbon.css":           "/admin/assets/carbon.css",
		"/admin//assets/./carbon.css/":      "/admin/assets/carbon.css",
		"/alias/../admin/assets/carbon.css": "/admin/assets/carbon.css",
		"/admin/assets/../../health":        "/health",
		"/admin\\assets/carbon.css":         "/admin\\assets/carbon.css",
	} {
		if got := CanonicalPath(input); got != want {
			t.Fatalf("CanonicalPath(%q) = %q, want %q", input, got, want)
		}
	}
}
