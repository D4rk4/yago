package pagefetch_test

import (
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
)

func TestAllowedContentType(t *testing.T) {
	allowed := []string{
		"text/html",
		"text/html; charset=utf-8",
		"application/xhtml+xml",
		"TEXT/HTML",
		`text/html; a="unterminated`,
	}
	for _, value := range allowed {
		if !pagefetch.AllowedContentType(value) {
			t.Errorf("AllowedContentType(%q) = false, want true", value)
		}
	}

	rejected := []string{
		"",
		"application/pdf",
		"image/png",
		"application/json",
		"text/plain; charset=utf-8",
		`application/pdf; a="unterminated`,
	}
	for _, value := range rejected {
		if pagefetch.AllowedContentType(value) {
			t.Errorf("AllowedContentType(%q) = true, want false", value)
		}
	}
}
