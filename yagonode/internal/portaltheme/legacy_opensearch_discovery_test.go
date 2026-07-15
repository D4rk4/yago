package portaltheme

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDefaultPortalDocumentsUseSharedOpenSearchTitle(t *testing.T) {
	for _, page := range []string{PageSearch, PageResults} {
		body := DefaultBody(page)
		if !strings.Contains(body, currentOpenSearchAdvertisement) ||
			strings.Contains(body, legacyOpenSearchAdvertisement) {
			t.Fatalf("default %q OpenSearch advertisement = %q", page, body)
		}
	}
}

func TestThemeRepairsOnlyLegacyOpenSearchAdvertisement(t *testing.T) {
	custom := `<p>{{brand}} search</p>`
	body := custom + legacyOpenSearchAdvertisement + custom
	for _, page := range []string{PageSearch, PageResults} {
		repaired := repairLegacyPortalDocument(page, body)
		if strings.Count(repaired, currentOpenSearchAdvertisement) != 1 ||
			strings.Contains(repaired, legacyOpenSearchAdvertisement) ||
			strings.Count(repaired, custom) != 2 {
			t.Fatalf("repaired %q body = %q", page, repaired)
		}
	}
	if repaired := repairLegacyPortalDocument(SharedStyles, body); repaired != body {
		t.Fatalf("styles repair changed OpenSearch markup: %q", repaired)
	}
}

func TestThemeLoadsLegacyOpenSearchAdvertisementAsDiscoverable(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	theme, err := Open(storage, nopSink{})
	if err != nil {
		t.Fatalf("open theme: %v", err)
	}
	legacy := strings.Replace(
		defaultSearchBody,
		currentOpenSearchAdvertisement,
		legacyOpenSearchAdvertisement,
		1,
	)
	if err := theme.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return theme.docs.Put(
			tx,
			vault.Key(PageSearch),
			Document{Body: legacy, ParseOK: true},
		)
	}); err != nil {
		t.Fatalf("store legacy theme: %v", err)
	}
	if err := theme.reload(context.Background()); err != nil {
		t.Fatalf("reload theme: %v", err)
	}
	if err := theme.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("enable theme: %v", err)
	}
	html, ok := theme.Render(t.Context(), PageSearch, map[string]any{
		"brand":           "YaGoSeek",
		"openSearchTitle": "YaGoSeek search",
	})
	if !ok || !strings.Contains(html, `title="YaGoSeek search" href="/opensearch.xml"`) {
		t.Fatalf("legacy theme render = %q, rendered=%t", html, ok)
	}
}
