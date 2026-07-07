package adminui

import (
	"net/http"
	"strings"
	"testing"
)

func TestPhotonStylesheetLinkedAndServed(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})

	page := do(t, console, "/admin/configuration")
	if !strings.Contains(page.body, `href="/admin/assets/photon.css"`) {
		t.Fatal("layout does not link the Photon stylesheet")
	}

	asset := do(t, console, "/admin/assets/photon.css")
	if asset.status != http.StatusOK {
		t.Fatalf("photon.css status %d", asset.status)
	}
	for _, want := range []string{"--ph-raise", ".cds-nav", "order: 2"} {
		if !strings.Contains(asset.body, want) {
			t.Fatalf("photon.css missing %q", want)
		}
	}
}

func TestNavIconsRenderFromSprite(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})
	got := do(t, console, "/admin/configuration")

	for _, icon := range []string{"overview", "search", "crawler", "network", "security", "logs"} {
		if !strings.Contains(got.body, `id="ic-`+icon+`"`) {
			t.Fatalf("sprite missing symbol %q", icon)
		}
		if !strings.Contains(got.body, `href="#ic-`+icon+`"`) {
			t.Fatalf("nav does not reference icon %q", icon)
		}
	}
	if !strings.Contains(got.body, `class="cds-nav__icon"`) {
		t.Fatal("nav icons missing their class")
	}
}
