package formatparse

import (
	"os"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestProbeRealMafetPDF(t *testing.T) {
	body, err := os.ReadFile("/home/dark/.claude/jobs/f4f8fb8a/tmp/msb.pdf")
	if err != nil {
		t.Skipf("no fixture: %v", err)
	}
	page, parsed := Parse(
		"http://mafet.org/msb/msb112999.pdf",
		"application/pdf",
		body,
		yagocrawlcontract.FormatToggles{PDF: true},
	)
	t.Logf("parsed=%v title=%q textLen=%d", parsed, page.Title, len(page.Text))
	if !parsed {
		t.Fatal("real pdf did not parse")
	}
}
