package yagonode

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

func porterController(t *testing.T) *blacklistController {
	t.Helper()
	store, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	denylist, err := urldenylist.Open(store, time.Now)
	if err != nil {
		t.Fatalf("open denylist: %v", err)
	}

	return newBlacklistController(denylist)
}

// TestBlacklistPorterRoundTrip pins UI-17: imported lines land as typed
// entries, the probe answers through the live matcher, and the export is
// itself importable.
func TestBlacklistPorterRoundTrip(t *testing.T) {
	controller := porterController(t)
	ctx := context.Background()

	added, err := controller.ImportBlacklist(ctx, strings.Join([]string{
		"# comment and blank lines are skipped",
		"",
		"domain spam.example",
		"url https://tracker.example/pixel",
		"barehost.example",
		"https://bare.example/full/url",
	}, "\n"))
	if err != nil || added != 4 {
		t.Fatalf("import = %d, %v", added, err)
	}

	if blocked, err := controller.BlacklistBlocks(
		ctx,
		"https://www.spam.example/page",
	); err != nil ||
		!blocked {
		t.Fatalf("subdomain of blocked domain: %v %v", blocked, err)
	}
	if blocked, _ := controller.BlacklistBlocks(ctx, "https://clean.example/"); blocked {
		t.Fatal("clean host must not be blocked")
	}

	payload, err := controller.ExportBlacklist(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, want := range []string{
		"domain spam.example", "url https://tracker.example/pixel",
		"domain barehost.example", "url https://bare.example/full/url",
	} {
		if !strings.Contains(payload, want+"\n") {
			t.Fatalf("export misses %q:\n%s", want, payload)
		}
	}
}

// TestBlacklistImportRejectsMalformedLineWithNumber pins the abort contract:
// a bad line stops the import and names its line number.
func TestBlacklistImportRejectsMalformedLineWithNumber(t *testing.T) {
	controller := porterController(t)
	added, err := controller.ImportBlacklist(context.Background(),
		"domain good.example\nbogus-kind value\n")
	if added != 1 || err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("import = %d, %v", added, err)
	}
}
