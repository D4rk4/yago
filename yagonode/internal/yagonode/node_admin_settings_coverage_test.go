package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

// reserveCodec is a throwaway codec used only to pre-register a bucket name.
type reserveCodec struct{}

func (reserveCodec) Encode(int) ([]byte, error) { return nil, nil }

func (reserveCodec) Decode([]byte) (int, error) { return 0, nil }

func TestLoadRuntimeSettingsIdentityError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	// Reserve the peer-identity bucket so resolving the identity fails on a
	// duplicate registration while the settings store still opens cleanly.
	if _, err := vault.Register(v, "peeridentity", reserveCodec{}); err != nil {
		t.Fatalf("reserve bucket: %v", err)
	}

	if _, _, _, err := loadRuntimeSettings(
		context.Background(), v, testConfig(t), nil,
	); err == nil {
		t.Fatal("loadRuntimeSettings should fail when peer identity cannot be resolved")
	}
}

func TestLoadRuntimeSettingsOpenError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, _, _, err := loadRuntimeSettings(
		context.Background(), v, testConfig(t), nil,
	); err == nil {
		t.Fatal("loadRuntimeSettings should fail on a closed store")
	}
}

func TestSettingsSourceReportsStoreErrors(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	src := newSettingsSource(store, testConfig(t), newRuntimeToggles(testConfig(t)), nil)
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	ctx := context.Background()
	if _, err := src.Update(ctx, adminui.SettingsChange{
		Key: settingKeyPublicSearchPortal, Value: settingBoolTrue,
	}); err == nil {
		t.Fatal("set should surface the store error")
	}
	if _, err := src.Update(ctx, adminui.SettingsChange{
		Key: settingKeyPublicSearchPortal, Reset: true,
	}); err == nil {
		t.Fatal("reset should surface the store error")
	}
}

func TestLoadRuntimeSettingsRejectsInvalidPersistedSecurityPolicy(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "authentication without secret",
			key:   settingKeyNetworkAuthenticationMode,
			value: string(yagoproto.NetworkAuthenticationSaltedMagic),
			want:  "network authentication requires a shared secret",
		},
		{
			name:  "incomplete remote crawl",
			key:   settingKeyRemoteCrawlEnabled,
			value: settingBoolTrue,
			want:  "remote crawl requires salted-magic network authentication",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newCtrlEngine()
			initial := ctrlVault(t, engine)
			store, err := settingsstore.Open(initial)
			if err != nil {
				t.Fatalf("settingsstore.Open: %v", err)
			}
			if err := store.Set(t.Context(), testCase.key, testCase.value); err != nil {
				t.Fatalf("Set: %v", err)
			}

			restarted := ctrlVault(t, engine)
			_, _, _, err = loadRuntimeSettings(t.Context(), restarted, testConfig(t), nil)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("loadRuntimeSettings error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestSettingsSourceReportsNetworkValidationReadErrors(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	source := newSettingsSource(store, testConfig(t), nil, nil)
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	changes := []adminui.SettingsChange{
		{
			Key:   settingKeyNetworkAuthenticationMode,
			Value: string(yagoproto.NetworkAuthenticationSaltedMagic),
		},
		{Key: settingKeyNetworkAuthenticationSecret, Reset: true},
	}
	for _, change := range changes {
		if _, err := source.Update(t.Context(), change); err == nil ||
			!strings.Contains(err.Error(), "load runtime settings") {
			t.Fatalf("Update(%+v) error = %v, want settings read failure", change, err)
		}
	}
}

func TestSettingsSourceReportsMutationErrors(t *testing.T) {
	cases := []struct {
		name   string
		change adminui.SettingsChange
		want   string
	}{
		{
			name: "set",
			change: adminui.SettingsChange{
				Key: settingKeyPublicSearchPortal, Value: settingBoolTrue,
			},
			want: "store setting",
		},
		{
			name: "unset",
			change: adminui.SettingsChange{
				Key: settingKeyPublicSearchPortal, Reset: true,
			},
			want: "clear setting",
		},
		{
			name: "privacy reset sentinel",
			change: adminui.SettingsChange{
				Key: settingKeyWebFallbackPrivacy, Reset: true,
			},
			want: "clear setting",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newCtrlEngine()
			storage := ctrlVault(t, engine)
			store, err := settingsstore.Open(storage)
			if err != nil {
				t.Fatalf("settingsstore.Open: %v", err)
			}
			source := newSettingsSource(store, testConfig(t), nil, nil)
			engine.failUpdate = true

			_, err = source.Update(t.Context(), testCase.change)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Update error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestSettingsSourceUpdateToleratesNilTogglesAndRecorder(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	src := newSettingsSource(store, testConfig(t), nil, nil)

	res, err := src.Update(context.Background(), adminui.SettingsChange{
		Key: settingKeyPublicSearchPortal, Value: settingBoolTrue,
	})
	if err != nil || !res.OK {
		t.Fatalf("update = %+v err=%v", res, err)
	}
}

func TestAdminSettingOptionsEmpty(t *testing.T) {
	if got := adminSettingOptions(nil); got != nil {
		t.Fatalf("adminSettingOptions(nil) = %v, want nil", got)
	}
}
