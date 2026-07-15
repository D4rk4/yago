package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
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

func TestSetupWizardApplierPersistsPeerName(t *testing.T) {
	settings := &recordingSettings{}
	applier := setupWizardApplier(settings)

	// A named node records the peer.name override, even in local-only mode.
	if err := applier(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModeLocal, WebFallback: "disabled", PeerName: "my-node",
	}); err != nil {
		t.Fatalf("named: %v", err)
	}
	if got := changedKeys(settings.changes)["peer.name"]; got != "my-node" {
		t.Fatalf("peer.name = %q, want my-node", got)
	}

	// An empty peer name keeps the generated name: no override is written.
	settings.changes = nil
	if err := applier(context.Background(), adminauth.SetupChoices{
		Mode: adminauth.SetupModeLocal, WebFallback: "disabled",
	}); err != nil {
		t.Fatalf("unnamed: %v", err)
	}
	if _, ok := changedKeys(settings.changes)["peer.name"]; ok {
		t.Fatal("empty peer name must not write a peer.name override")
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
	configureSetupWizard(service, nil, nodeConfig{}, nil)
}

// TestConfigureSetupWizardArmsMandatoryRestart drives the real setup form:
// once the wizard applies its choices, the armed restart trigger fires and the
// operator sees the restarting notice instead of a login redirect served by a
// half-configured node.
func TestWizardSeedlistDefaultPrefillsCanonicalWhenEmpty(t *testing.T) {
	if got := wizardSeedlistDefault(nil); got != strings.Join(defaultWizardSeedlistURLs, ",") {
		t.Errorf("empty config should prefill the canonical seeds, got %q", got)
	}
	if len(defaultWizardSeedlistURLs) == 0 {
		t.Fatal("canonical seed list must not be empty")
	}
	configured := []string{"http://a.example/seed.txt", "http://b.example/seed.txt"}
	if got := wizardSeedlistDefault(configured); got != strings.Join(configured, ",") {
		t.Errorf("configured seeds should win, got %q", got)
	}
}

func TestConfigureSetupWizardArmsMandatoryRestart(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	service, err := adminauth.New(storage, adminauth.Config{})
	if err != nil {
		t.Fatalf("adminauth.New: %v", err)
	}
	restarts := 0
	configureSetupWizard(service, &recordingSettings{}, nodeConfig{}, func() { restarts++ })

	mux := http.NewServeMux()
	adminauth.MountHTML(mux, service)
	setupPage := httptest.NewRecorder()
	setupPageRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		adminauth.PathSetupPage,
		nil,
	)
	mux.ServeHTTP(setupPage, setupPageRequest)
	setupCookie := setupPage.Result().Cookies()[0]
	const fieldPrefix = `name="setup_token" value="`
	tokenStart := strings.Index(setupPage.Body.String(), fieldPrefix)
	if tokenStart < 0 {
		t.Fatal("setup token missing")
	}
	tokenRemainder := setupPage.Body.String()[tokenStart+len(fieldPrefix):]
	tokenEnd := strings.IndexByte(tokenRemainder, '"')
	if tokenEnd < 0 {
		t.Fatal("setup token malformed")
	}
	form := url.Values{
		"username":    {"admin"},
		"password":    {"correct horse battery staple"},
		"mode":        {"search"},
		"setup_token": {tokenRemainder[:tokenEnd]},
	}
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		adminauth.PathSetupPage,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(setupCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Restarting") {
		t.Fatalf("setup = %d, want the restarting page", rec.Code)
	}
	if restarts != 1 {
		t.Fatalf("restart triggered %d times, want 1", restarts)
	}
}
