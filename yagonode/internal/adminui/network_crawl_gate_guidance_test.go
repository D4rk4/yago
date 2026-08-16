package adminui

import (
	"strings"
	"testing"
)

func TestNetworkCrawlGateGuidance(t *testing.T) {
	blocked := do(t, New(Options{Network: fakeNetwork{snap: NetworkStatus{
		Available:               true,
		RosterAvailable:         true,
		BlockingReason:          "crawl is in progress",
		CrawlBlocksDistribution: true,
	}}}), "/admin/network")
	for _, want := range []string{
		"Automatic and swarm-seed profiles count as active crawls.",
		`href="/admin/configuration#panel-swarm"`,
		"enable Distribute while crawling",
		"restart the node",
	} {
		if !strings.Contains(blocked.body, want) {
			t.Fatalf("crawl-blocked Network page missing %q", want)
		}
	}

	other := do(t, New(Options{Network: fakeNetwork{snap: NetworkStatus{
		Available:       true,
		RosterAvailable: true,
		BlockingReason:  "network is too small",
	}}}), "/admin/network")
	if strings.Contains(other.body, "Automatic and swarm-seed profiles") ||
		strings.Contains(other.body, "/admin/configuration#panel-swarm") {
		t.Fatal("non-crawl gate rendered crawl remediation")
	}
}
