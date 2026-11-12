package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeFreelistCheckpointer struct {
	result FreelistCheckpointResult
	err    error
	calls  int
}

func (f *fakeFreelistCheckpointer) CheckpointFreelists(context.Context) (FreelistCheckpointResult, error) {
	f.calls++

	return f.result, f.err
}

func configWithFreelistCheckpoint(checkpointer FreelistCheckpointer) *Console {
	return New(Options{
		Config:               fakeConfig{view: ConfigView{}},
		Settings:             &fakeSettings{view: storageSettingsView()},
		FreelistCheckpointer: checkpointer,
	})
}

func TestConfigRendersFreelistCheckpointInStorageTab(t *testing.T) {
	t.Parallel()

	got := do(t, configWithFreelistCheckpoint(&fakeFreelistCheckpointer{}), configPath)
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Save freelists now",
		`name="form" value="checkpoint-freelists"`,
		`name="csrf_token"`,
		"Vault reads and writes pause",
		`id="panel-storage"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("config page missing %q", want)
		}
	}
}

func TestConfigHidesFreelistCheckpointWhenUnavailable(t *testing.T) {
	t.Parallel()

	got := do(t, configWithFreelistCheckpoint(nil), configPath)
	if got.status != http.StatusOK || strings.Contains(got.body, "Save freelists now") {
		t.Fatalf("unavailable checkpoint control rendered: status=%d body=%s", got.status, got.body)
	}
}

func TestConfigFreelistCheckpointOnlyRendersInStorageTab(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config: fakeConfig{view: ConfigView{}},
		Settings: &fakeSettings{view: SettingsView{Items: []SettingItem{{
			Key: "search.example", Title: "Example", Category: "Search",
		}}}},
		FreelistCheckpointer: &fakeFreelistCheckpointer{},
	})
	got := do(t, console, configPath)
	if got.status != http.StatusOK || strings.Contains(got.body, "Save freelists now") {
		t.Fatalf("checkpoint appeared without a Storage tab: status=%d body=%s", got.status, got.body)
	}
}

func TestConfigFreelistCheckpointReportsSuccess(t *testing.T) {
	t.Parallel()

	checkpointer := &fakeFreelistCheckpointer{
		result: FreelistCheckpointResult{ShardsCheckpointed: 8, ShardsTotal: 8},
	}
	got := doPost(t, configWithFreelistCheckpoint(checkpointer), configPath,
		url.Values{"form": {"checkpoint-freelists"}},
	)
	if got.status != http.StatusOK || checkpointer.calls != 1 ||
		!strings.Contains(got.body, "Saved freelists for 8 shard(s). Vault operations have resumed.") {
		t.Fatalf("checkpoint result status=%d calls=%d body=%s", got.status, checkpointer.calls, got.body)
	}
}

func TestConfigFreelistCheckpointReportsPartialFailure(t *testing.T) {
	t.Parallel()

	checkpointer := &fakeFreelistCheckpointer{
		result: FreelistCheckpointResult{ShardsCheckpointed: 6, ShardsTotal: 8},
		err:    errors.New("private shard detail"),
	}
	got := doPost(t, configWithFreelistCheckpoint(checkpointer), configPath,
		url.Values{"form": {"checkpoint-freelists"}},
	)
	if got.status != http.StatusOK || checkpointer.calls != 1 ||
		!strings.Contains(got.body, "Vault checkpoint saved 6 of 8 shard freelists.") ||
		strings.Contains(got.body, "private shard detail") {
		t.Fatalf("checkpoint error status=%d calls=%d body=%s", got.status, checkpointer.calls, got.body)
	}
}

func TestConfigFreelistCheckpointUnavailablePostNotFound(t *testing.T) {
	t.Parallel()

	got := doPost(t, configWithFreelistCheckpoint(nil), configPath,
		url.Values{"form": {"checkpoint-freelists"}},
	)
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want %d", got.status, http.StatusNotFound)
	}
}
