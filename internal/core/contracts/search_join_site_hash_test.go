package contracts

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestJoinSiteHashHashesModifierSite(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{Modifier: "site:example.com /language/de"}}
	want := yacymodel.HostHashFromName("example.com")
	if got := query.JoinSiteHash(); got != want {
		t.Fatalf("join site hash = %q, want %q", got, want)
	}
}

func TestJoinSiteHashPrefersTransmittedHash(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{SiteHash: "abcdef", Modifier: "site:example.com"}}
	if got := query.JoinSiteHash(); got != "abcdef" {
		t.Fatalf("join site hash = %q, want abcdef", got)
	}
}
