package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeCompactor struct {
	result CompactionResult
	err    error
	calls  int
}

func (f *fakeCompactor) Compact(context.Context) (CompactionResult, error) {
	f.calls++

	return f.result, f.err
}

func storageSettingsView() SettingsView {
	return SettingsView{Items: []SettingItem{{
		Key:      "storage.compaction.interval",
		Title:    "Compaction interval",
		Value:    "1d",
		Category: "Storage",
	}}}
}

func configWithStorage(compactor Compactor) *Console {
	return New(Options{
		Config:    fakeConfig{view: ConfigView{}},
		Settings:  &fakeSettings{view: storageSettingsView()},
		Compactor: compactor,
	})
}

func TestConfigRendersCompactButtonInStorageTab(t *testing.T) {
	t.Parallel()

	got := do(t, configWithStorage(&fakeCompactor{}), configPath)
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"Compact now", `name="form" value="compact"`, `id="panel-storage"`} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("config page missing %q", want)
		}
	}
}

func TestConfigHidesCompactWithoutCompactor(t *testing.T) {
	t.Parallel()

	got := do(t, configWithStorage(nil), configPath)
	if strings.Contains(got.body, "Compact now") {
		t.Fatal("Compact now must not render without a compactor")
	}
}

func TestConfigCompactButtonOnlyInStorageTab(t *testing.T) {
	t.Parallel()

	// A settings surface with no Storage category yields no storage panel, so the
	// compact action has nowhere to render even with a compactor wired.
	view := SettingsView{Items: []SettingItem{{Key: "search.x", Title: "X", Category: "Search"}}}
	console := New(Options{
		Config:    fakeConfig{view: ConfigView{}},
		Settings:  &fakeSettings{view: view},
		Compactor: &fakeCompactor{},
	})
	if strings.Contains(do(t, console, configPath).body, "Compact now") {
		t.Fatal("Compact now must only render in the Storage tab")
	}
}

func TestConfigCompactShowsReclaimedToast(t *testing.T) {
	t.Parallel()

	compactor := &fakeCompactor{
		result: CompactionResult{ShardsCompacted: 3, BytesReclaimed: "12.0 MiB"},
	}
	got := doPost(t, configWithStorage(compactor), configPath, url.Values{"form": {"compact"}})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if compactor.calls != 1 {
		t.Fatalf("compact calls = %d, want 1", compactor.calls)
	}
	if !strings.Contains(got.body, "Reclaimed 12.0 MiB across 3 shard(s).") {
		t.Fatalf("missing reclaim toast; body: %s", got.body)
	}
}

func TestConfigCompactShowsNothingReclaimed(t *testing.T) {
	t.Parallel()

	compactor := &fakeCompactor{result: CompactionResult{ShardsCompacted: 0, BytesReclaimed: "0 B"}}
	got := doPost(t, configWithStorage(compactor), configPath, url.Values{"form": {"compact"}})
	if got.status != http.StatusOK || !strings.Contains(got.body, "already compact") {
		t.Fatalf("status %d, missing already-compact toast", got.status)
	}
}

func TestConfigCompactShowsError(t *testing.T) {
	t.Parallel()

	compactor := &fakeCompactor{err: errors.New("shard busy")}
	got := doPost(t, configWithStorage(compactor), configPath, url.Values{"form": {"compact"}})
	if got.status != http.StatusOK || !strings.Contains(got.body, "Compaction failed.") {
		t.Fatalf("status %d, missing failure toast", got.status)
	}
}

func TestConfigCompactWithoutCompactorNotFound(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{Config: fakeConfig{view: ConfigView{}}}),
		configPath, url.Values{"form": {"compact"}})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}
