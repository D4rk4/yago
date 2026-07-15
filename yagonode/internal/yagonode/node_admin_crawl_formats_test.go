package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlformats"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestCrawlFormatsSourceRoundTrip(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := crawlformats.Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	source := crawlFormatsSource{store: store}
	ctx := context.Background()

	current, err := source.CurrentFormats(ctx)
	if err != nil {
		t.Fatalf("current defaults: %v", err)
	}
	if !current.Text || current.Archives {
		t.Fatalf("defaults = %+v", current)
	}
	err = source.SaveFormats(ctx, adminui.FormatSettings{
		Text: true, XMLFeeds: true, PDF: true, Office: true,
		Images: true, Audio: true, Misc: true, Archives: true,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := source.CurrentFormats(ctx)
	if err != nil {
		t.Fatalf("current saved: %v", err)
	}
	if !got.Archives || !got.Misc {
		t.Fatalf("saved = %+v", got)
	}
}

func TestBuildCrawlRuntimeFormatsOpenError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProvision["crawl_formats"] = true
	v := ctrlVault(t, engine)

	_, err := buildRuntimeCrawl(
		crawlConfig{ListenAddr: "127.0.0.1:0"},
		nodeIdentity(testConfig(t)),
		nodeStorage{},
		v,
	)
	if err == nil {
		t.Fatal("buildCrawlRuntime should surface the crawl formats open error")
	}
}

func TestCrawlFormatsSourceSurfacesPersistedReadError(t *testing.T) {
	engine := newCtrlEngine()
	v := ctrlVault(t, engine)
	store, err := crawlformats.Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Set(
		context.Background(),
		yagocrawlcontract.DefaultFormatToggles(),
	); err != nil {
		t.Fatalf("seed formats: %v", err)
	}
	engine.corrupt("crawl_formats")

	_, err = (crawlFormatsSource{store: store}).CurrentFormats(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read crawl formats") {
		t.Fatalf("CurrentFormats error = %v, want persisted read error", err)
	}
}
