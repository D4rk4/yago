package adminui

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"General":         "general",
		"Network & peers": "network-peers",
		"Web fallback":    "web-fallback",
		"  spaced  ":      "spaced",
		"A/B  C":          "a-b-c",
		"!!!":             "",
		"HTTP2":           "http2",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupSettingsOrdersAndBuckets(t *testing.T) {
	t.Parallel()

	items := []SettingItem{
		{Key: "search.a", Category: "Search"},
		{Key: "peer.a"}, // empty category -> General
		{Key: "z.custom", Category: "Zebra"},
		{Key: "search.b", Category: "Search"},
		{Key: "net.a", Category: "Network & peers"},
	}
	groups := groupSettings(items)

	titles := make([]string, 0, len(groups))
	for _, g := range groups {
		titles = append(titles, g.Title)
	}
	// General precedes Search precedes Network & peers per settingGroupOrder;
	// the unlisted "Zebra" category trails in first-seen order.
	want := []string{"General", "Search", "Network & peers", "Zebra"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Fatalf("group order = %v, want %v", titles, want)
	}

	var search *SettingGroup
	for i := range groups {
		if groups[i].Title == "Search" {
			search = &groups[i]
		}
	}
	if search == nil || len(search.Items) != 2 ||
		search.Items[0].Key != "search.a" || search.Items[1].Key != "search.b" {
		t.Fatalf("search bucket lost order: %+v", search)
	}
	if search.ID != "search" {
		t.Fatalf("search group id = %q", search.ID)
	}
}

func TestGroupSettingsEmpty(t *testing.T) {
	t.Parallel()

	if groups := groupSettings(nil); len(groups) != 0 {
		t.Fatalf("expected no groups, got %v", groups)
	}
}
