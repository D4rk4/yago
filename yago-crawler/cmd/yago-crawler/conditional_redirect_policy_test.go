package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestRedirectPolicyStripsConditionalValidatorsOnEveryHop(t *testing.T) {
	policy := dynamicRedirectPolicy(newRedirectLimit(1))
	for _, tc := range []struct {
		name     string
		previous int
		refused  bool
	}{
		{name: "admitted hop", previous: 1},
		{name: "hop over the limit", previous: 2, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, err := http.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				"https://example.org/moved",
				nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			next.Header.Set("If-None-Match", `"v1"`)
			next.Header.Set("If-Modified-Since", "Wed, 01 Jul 2026 10:00:00 GMT")
			next.Header.Set("User-Agent", "crawler/1.0")
			previous := make([]*http.Request, tc.previous)
			for i := range previous {
				previous[i] = next
			}

			err = policy(next, previous)
			if refused := errors.Is(err, errRedirectLimitReached); refused != tc.refused {
				t.Fatalf("policy error = %v, want refused %v", err, tc.refused)
			}
			if _, sent := next.Header["If-None-Match"]; sent {
				t.Fatal("If-None-Match survived the redirect hop")
			}
			if _, sent := next.Header["If-Modified-Since"]; sent {
				t.Fatal("If-Modified-Since survived the redirect hop")
			}
			if next.Header.Get("User-Agent") != "crawler/1.0" {
				t.Fatalf("user agent = %q, want it kept", next.Header.Get("User-Agent"))
			}
		})
	}
}
