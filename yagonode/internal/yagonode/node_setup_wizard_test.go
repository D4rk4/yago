package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type recordingSettings struct {
	changes []adminui.SettingsChange
	fail    string
	err     error
}

func (r *recordingSettings) Settings(context.Context) adminui.SettingsView {
	return adminui.SettingsView{}
}

func (r *recordingSettings) Update(
	_ context.Context,
	change adminui.SettingsChange,
) (adminui.SettingsResult, error) {
	if r.err != nil && change.Key == r.fail {
		return adminui.SettingsResult{}, r.err
	}
	if r.fail != "" && r.err == nil && change.Key == r.fail {
		return adminui.SettingsResult{OK: false, Message: "rejected"}, nil
	}
	r.changes = append(r.changes, change)

	return adminui.SettingsResult{OK: true}, nil
}

func changedKeys(changes []adminui.SettingsChange) map[string]string {
	out := make(map[string]string, len(changes))
	for _, change := range changes {
		out[change.Key] = change.Value
	}

	return out
}

func TestSetupWizardApplierModes(t *testing.T) {
	settings := &recordingSettings{}
	applier := setupWizardApplier(settings)

	// Local-only records the fallback choice and keeps the portal off.
	err := applier(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModeLocal, WebFallback: "disabled",
		AdvertiseHost: "ignored.example",
	})
	if err != nil {
		t.Fatalf("local: %v", err)
	}
	local := changedKeys(settings.changes)
	if local[settingKeyPublicSearchPortal] != settingBoolFalse {
		t.Fatalf("local portal = %q", local[settingKeyPublicSearchPortal])
	}
	if _, ok := local["network.advertise.host"]; ok {
		t.Fatal("local mode must not record the advertise host")
	}

	// Search-node mode records everything and switches the portal on.
	settings.changes = nil
	err = applier(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModeSearchNode, WebFallback: "enabled",
		AdvertiseHost: "pub.example", Seedlists: "https://seeds.example/a",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	search := changedKeys(settings.changes)
	if search[settingKeyPublicSearchPortal] != settingBoolTrue ||
		search["network.advertise.host"] != "pub.example" ||
		search["network.seedlists"] != "https://seeds.example/a" ||
		search["web.fallback.privacy"] != "enabled" {
		t.Fatalf("search changes = %+v", search)
	}

	// Peer mode records the network settings but keeps the portal off.
	settings.changes = nil
	if err := applier(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModePublicPeer, AdvertiseHost: "peer.example",
	}); err != nil {
		t.Fatalf("peer: %v", err)
	}
	peer := changedKeys(settings.changes)
	if peer[settingKeyPublicSearchPortal] != settingBoolFalse ||
		peer["network.advertise.host"] != "peer.example" {
		t.Fatalf("peer changes = %+v", peer)
	}
}

func TestSetupWizardApplierSurfacesFailures(t *testing.T) {
	broken := &recordingSettings{fail: "network.seedlists", err: errors.New("store down")}
	err := setupWizardApplier(broken)(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModePublicPeer,
	})
	if err == nil {
		t.Fatal("store failure must surface")
	}

	rejected := &recordingSettings{fail: "web.fallback.privacy"}
	err = setupWizardApplier(rejected)(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModeLocal, WebFallback: "bogus",
	})
	if err == nil {
		t.Fatal("rejected change must surface")
	}
}

func TestConfigureSetupWizardSkipsNilSettings(t *testing.T) {
	service := &adminauth.Service{}
	configureSetupWizard(service, nil, nodeConfig{})
}
