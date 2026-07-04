package crawljob_test

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

func TestDiscoveredLinksByPolicySkipsNoFollow(t *testing.T) {
	links := crawljob.DiscoveredLinks{
		Followable: []string{"https://example.com/follow"},
		NoFollow:   []string{"https://example.com/blocked"},
	}
	got := links.ByPolicy(false)
	if !slices.Equal(got, []string{"https://example.com/follow"}) {
		t.Fatalf("links = %v", got)
	}
	got[0] = "mutated"
	if links.Followable[0] != "https://example.com/follow" {
		t.Fatalf("followable links mutated: %v", links.Followable)
	}
}

func TestDiscoveredLinksByPolicyIncludesNoFollow(t *testing.T) {
	links := crawljob.DiscoveredLinks{
		Followable: []string{"https://example.com/follow"},
		NoFollow:   []string{"https://example.com/blocked"},
	}
	got := links.ByPolicy(true)
	want := []string{"https://example.com/follow", "https://example.com/blocked"}
	if !slices.Equal(got, want) {
		t.Fatalf("links = %v want %v", got, want)
	}
}
