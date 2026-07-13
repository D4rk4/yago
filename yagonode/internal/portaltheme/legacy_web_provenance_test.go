package portaltheme

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestThemeRepairsLegacyDefaultWebProvenance(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	theme, err := Open(storage, nopSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	legacy := map[string]string{
		PageSearch: strings.ReplaceAll(
			defaultSearchBody,
			currentWebFooterFragment,
			legacyWebFooterFragment,
		),
		PageResults: strings.ReplaceAll(
			strings.ReplaceAll(
				defaultResultsBody,
				currentWebFooterFragment,
				legacyWebFooterFragment,
			),
			currentWebCountFragment,
			legacyWebCountFragment,
		),
		SharedStyles: strings.ReplaceAll(
			defaultStylesBody,
			currentWebStyleFragment,
			legacyWebStyleFragment,
		),
	}
	if err := theme.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for page, body := range legacy {
			if err := theme.docs.Put(
				tx,
				vault.Key(page),
				Document{Body: body, ParseOK: true},
			); err != nil {
				return fmt.Errorf("store legacy document %q: %w", page, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("store legacy theme: %v", err)
	}
	if err := theme.reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	for page := range legacy {
		document, found, err := theme.Document(t.Context(), page)
		if err != nil || !found {
			t.Fatalf("document %q found=%t error=%v", page, found, err)
		}
		if strings.Contains(strings.ToLower(document.Body), "ddgs") ||
			!strings.Contains(document.Body, "web") {
			t.Fatalf("document %q retained legacy provenance: %q", page, document.Body)
		}
	}
}

func TestThemeRepairsOnlyKnownLegacyWebFragments(t *testing.T) {
	body := "<header>custom</header>" + legacyWebFooterFragment +
		"<aside>operator ddgs note</aside>"
	repaired := repairLegacyPortalDocument(PageSearch, body)
	if !strings.Contains(repaired, "<header>custom</header>") ||
		!strings.Contains(repaired, "<aside>operator ddgs note</aside>") ||
		!strings.Contains(repaired, currentWebFooterFragment) ||
		strings.Contains(repaired, legacyWebFooterFragment) {
		t.Fatalf("repaired body = %q", repaired)
	}
}
