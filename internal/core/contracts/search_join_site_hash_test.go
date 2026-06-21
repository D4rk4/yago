package contracts

import (
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestJoinSiteHashHashesModifierSite(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{Modifier: "site:example.com /language/de"}}
	want, err := yacymodel.HashURLHost("example.com")
	if err != nil {
		t.Fatalf("HashURLHost: %v", err)
	}
	wantHash, err := want.HostHash()
	if err != nil {
		t.Fatalf("HostHash: %v", err)
	}
	got, err := query.JoinSiteHash()
	if err != nil {
		t.Fatalf("JoinSiteHash: %v", err)
	}
	if got != wantHash {
		t.Fatalf("join site hash = %q, want %q", got, wantHash)
	}
}

func TestJoinSiteHashPrefersTransmittedHash(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{SiteHash: "abcdef", Modifier: "site:example.com"}}
	if got, err := query.JoinSiteHash(); err != nil || got != "abcdef" {
		t.Fatalf("join site hash = %q, want abcdef", got)
	}
}

func TestJoinSiteHashRejectsInvalidSiteHost(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{Modifier: "site:%%%"}}
	if got, err := query.JoinSiteHash(); err == nil ||
		!strings.Contains(err.Error(), "parse url host") ||
		got != "" {
		t.Fatalf("JoinSiteHash() = %q, %v, want invalid host error", got, err)
	}
}
