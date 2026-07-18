package formatparse

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestParseKeepsUnknownTextFamilyFallback(t *testing.T) {
	page, parsed := Parse(
		"https://example.test/README.custom",
		"text/x-readme",
		[]byte("portable documentation"),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "portable documentation") {
		t.Fatalf("unknown text parse = %t %+v", parsed, page)
	}
}
