package adminui

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestHaikuNavigationIconsMatchPinnedAssets(t *testing.T) {
	t.Parallel()

	want := pinnedHaikuIconDigests()
	if len(navItems) != 14 {
		t.Fatalf("navigation item count = %d, want 14", len(navItems))
	}
	if len(want) != 15 {
		t.Fatalf("pinned icon count = %d, want 15", len(want))
	}
	assertPinnedHaikuAssets(t, want)
	assertUniqueNavigationIcons(t, want)
	assertSystemMonitorIcon(t)
	assertHaikuIconLicense(t)
	assertNavigationIconColors(t)
}

func pinnedHaikuIconDigests() map[string]string {
	return map[string]string{
		"icons/desktop.svg":                  "77edf404b355cd95ab86eebe89620bd9a2082f4ab37333c69569e4a28a0f75aa",
		"icons/system-search.svg":            "8dadf8c346529d0a2682a7037d1af5049c84c9c9b20c8453ffc5b01ce0856bf1",
		"icons/appointment.svg":              "afa83755d6ef9d39c8e47a6668309d05bede49fae28a5dd669d12c4e8c093ee1",
		"icons/applications-internet.svg":    "b5d5c68cb0ca53dbc92827e9b362f62e155632a4a12ce5dc329b8a24dbca1343",
		"icons/gnome-robots.svg":             "1e0394b41df50e42f654bfbc518683a8af8b9f08e3b19161e037ea741da3cc0d",
		"icons/network-workgroup.svg":        "db511085881e4e9da8a4b35d23661d6ac4c10b58dfcd93dab000ed076e3b3ebe",
		"icons/drive-partition.svg":          "750042735fbeb5ffcbb31aabd33f0c71a7eb101ba10783e10173892872aca8d7",
		"icons/applications-science.svg":     "9927035c98c9644b08fe8c3b3f39e61646554a5992e652238e97504e27dc3970",
		"icons/speedometer.svg":              "3f0f79ca4859bdb01a638e96e08ef1da07d486e9b18b2e8f263e8bd528f12350",
		"icons/utilities-system-monitor.svg": "3c09dc945b3b1cdf21084d9483ffb3b1a8853772528dbcb8a5e9cdfdc7b8e175",
		"icons/media-floppy.svg":             "964bd85894630c65c57c9ec61442af6f4359787708e3c98ae00cd0d544073afa",
		"icons/preferences-system.svg":       "dfc855802cd58550c0b9cb736108813c11889c0599b1f4f3f7830f82a63cad48",
		"icons/security-high.svg":            "7f7a182f456934e8f238ed52d0b13e8e5c7848f8a177a98cf4f46fdd43cd3db6",
		"icons/accessories-text-editor.svg":  "fe1e9872e7ca3ae23e23b5e17b7702121ce1edbeb82ebb6f56ac7f5d2a7b0122",
		"icons/view-refresh.svg":             "0f05dcf9e7147aa40f5f06f395a8aedb8bad1be33d10362d2fc8c4b7050b1542",
	}
}

func assertPinnedHaikuAssets(t *testing.T, want map[string]string) {
	t.Helper()
	for icon, expected := range want {
		content, err := assetFS.ReadFile("assets/" + icon)
		if err != nil {
			t.Fatal(err)
		}
		if digest := fmt.Sprintf("%x", sha256.Sum256(content)); digest != expected {
			t.Fatalf("%s digest = %s", icon, digest)
		}
		if strings.Contains(string(content), "currentColor") {
			t.Fatalf("%s relies on currentColor", icon)
		}
	}
}

func assertUniqueNavigationIcons(t *testing.T, want map[string]string) {
	t.Helper()
	seen := make(map[string]int, len(navItems))
	for _, item := range navItems {
		if _, known := want[item.Icon]; !known {
			t.Fatalf("untracked navigation icon %q", item.Icon)
		}
		seen[item.Icon]++
	}
	if len(seen) != 14 {
		t.Fatalf("navigation unique icon count = %d, want 14: %v", len(seen), seen)
	}
	for icon, uses := range seen {
		if uses != 1 {
			t.Fatalf("navigation icon %s uses = %d, want 1", icon, uses)
		}
	}
}

func assertSystemMonitorIcon(t *testing.T) {
	t.Helper()
	heading, err := templateFS.ReadFile("templates/system_monitor.tmpl")
	if err != nil || !strings.Contains(
		string(heading),
		`{{asset "icons/utilities-system-monitor.svg"}}`,
	) {
		t.Fatalf("System Monitor heading icon = %q, %v", heading, err)
	}
}

func assertHaikuIconLicense(t *testing.T) {
	t.Helper()
	license, err := assetFS.ReadFile("assets/icons/HAIKU-ICONS-LICENSE.txt")
	if err != nil || !strings.Contains(string(license), "MIT/X Consortium License") ||
		!strings.Contains(string(license), "© 2021 phillbush") {
		t.Fatalf("Haiku icon license = %q, %v", license, err)
	}
}

func assertNavigationIconColors(t *testing.T) {
	t.Helper()
	styles, err := assetFS.ReadFile("assets/photon.css")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(styles), ".cds-nav__icon { filter:") ||
		strings.Contains(string(styles), ".cds-nav__icon { color:") {
		t.Fatal("navigation styling flattens the color icon assets")
	}
}
