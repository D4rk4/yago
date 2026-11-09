package formatparse

import (
	"strings"
	"testing"
	"time"
)

func TestArchiveReadingTimeLimitStopsBeforeTheNextEntry(t *testing.T) {
	body := tarBody(t, map[string]string{
		"first.txt":  "First harbour survey notes",
		"second.txt": "Second harbour survey notes",
		"third.txt":  "Third harbour survey notes",
	})
	page, parsed := parseArchiveWithin(0, "https://a.example/surveys.tar", body)
	if !parsed || strings.Count(page.Text, ".txt") != 1 {
		t.Fatalf("expired archive read = %t %q", parsed, page.Text)
	}
	page, parsed = parseArchiveWithin(time.Minute, "https://a.example/surveys.tar", body)
	if !parsed || strings.Count(page.Text, ".txt") != 3 {
		t.Fatalf("archive read within its limit = %t %q", parsed, page.Text)
	}
}
