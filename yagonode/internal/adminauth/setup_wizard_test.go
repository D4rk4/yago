package adminauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWizardChoicesNormalizesMode(t *testing.T) {
	form := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	choices := wizardChoices(form(map[string]string{
		"mode": "search", "peer_name": "  my-node  ", "advertise_host": " host.example ",
		"seedlists": "https://s.example/a", "web_fallback": "enabled",
	}))
	if choices.Mode != SetupModeSearchNode || choices.AdvertiseHost != "host.example" {
		t.Fatalf("choices = %+v", choices)
	}
	if choices.PeerName != "my-node" {
		t.Fatalf("peer name = %q, want the trimmed value", choices.PeerName)
	}
	if got := wizardChoices(form(map[string]string{"mode": "bogus"})); got.Mode != SetupModeLocal {
		t.Fatalf("bogus mode = %q", got.Mode)
	}
	if got := wizardChoices(
		form(map[string]string{"mode": "peer"}),
	); got.Mode != SetupModePublicPeer {
		t.Fatalf("peer mode = %q", got.Mode)
	}
}

// doAuthRequest serves one request through the mounted HTML auth pages.
func doAuthRequest(
	t *testing.T,
	service *Service,
	method string,
	form url.Values,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	MountHTML(mux, service)
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(t.Context(), method, PathSetupPage, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestSetupPageShowsWizardWhenConfigured(t *testing.T) {
	service := testService(t)
	service.ConfigureSetupWizard(SetupDefaults{
		PeerName:      "my-node",
		AdvertiseHost: "auto.example",
		Seedlists:     "https://seeds.example/a",
		WebFallback:   "disabled",
	}, func(context.Context, SetupChoices) error { return nil })

	rec := doAuthRequest(t, service, http.MethodGet, nil)
	body := rec.Body.String()
	for _, want := range []string{
		"Node mode", `value="local" checked`, `value="peer"`, `value="search"`,
		`name="peer_name"`, `value="my-node"`,
		`value="auto.example"`, `value="https://seeds.example/a"`,
		"Web search fallback", `value="always"`, "Always",
		// The mode radios must reset the base full-width input geometry, else the
		// tall 2.5rem input box pushes each label off its radio (regression: the
		// wizard's "Node mode" choices rendered with the text wrapped below the
		// button). A flex row keeps radio and label on one baseline.
		".modes label { display: flex;",
		".modes input { width: auto; height: auto;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("setup page missing %q", want)
		}
	}
}

func TestSetupFormAppliesWizardChoices(t *testing.T) {
	service := testService(t)
	var applied SetupChoices
	service.ConfigureSetupWizard(
		SetupDefaults{},
		func(_ context.Context, choices SetupChoices) error {
			applied = choices

			return nil
		},
	)

	rec := doAuthRequest(t, service, http.MethodPost, url.Values{
		"username":       {"admin"},
		"password":       {"correct horse battery staple"},
		"mode":           {"search"},
		"peer_name":      {"my-node"},
		"advertise_host": {"pub.example"},
		"seedlists":      {"https://seeds.example/a"},
		"web_fallback":   {"explicit"},
	})
	if rec.Code != http.StatusSeeOther ||
		!strings.Contains(rec.Header().Get("Location"), "notice=created") {
		t.Fatalf("setup = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if applied.Mode != SetupModeSearchNode || applied.AdvertiseHost != "pub.example" ||
		applied.WebFallback != "explicit" || applied.PeerName != "my-node" {
		t.Fatalf("applied = %+v", applied)
	}
}

func TestSetupFormSurfacesWizardFailure(t *testing.T) {
	service := testService(t)
	service.ConfigureSetupWizard(SetupDefaults{}, func(context.Context, SetupChoices) error {
		return errors.New("store down")
	})

	rec := doAuthRequest(t, service, http.MethodPost, url.Values{
		"username": {"admin"},
		"password": {"correct horse battery staple"},
	})
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "notice=created") ||
		!strings.Contains(location, "error=wizard") {
		t.Fatalf("partial success location = %q", location)
	}
}

// TestSetupFormEndsInMandatoryRestart: with a restart trigger armed, a
// successful wizard apply renders the restarting notice and invokes the
// trigger, while a failed apply keeps the node running.
func TestSetupFormEndsInMandatoryRestart(t *testing.T) {
	service := testService(t)
	service.ConfigureSetupWizard(
		SetupDefaults{},
		func(context.Context, SetupChoices) error { return nil },
	)
	restarts := 0
	service.ConfigureSetupRestart(func() { restarts++ })

	rec := doAuthRequest(t, service, http.MethodPost, url.Values{
		"username": {"admin"},
		"password": {"correct horse battery staple"},
		"mode":     {"search"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup = %d, want the restarting page", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Restarting", `http-equiv="refresh"`, "/admin/login"} {
		if !strings.Contains(body, want) {
			t.Fatalf("restarting page missing %q", want)
		}
	}
	if restarts != 1 {
		t.Fatalf("restart triggered %d times, want 1", restarts)
	}
}

func TestSetupFormSkipsRestartWhenWizardFails(t *testing.T) {
	service := testService(t)
	service.ConfigureSetupWizard(
		SetupDefaults{},
		func(context.Context, SetupChoices) error { return errors.New("apply failed") },
	)
	service.ConfigureSetupRestart(func() { t.Fatal("restart must not run on a failed apply") })

	rec := doAuthRequest(t, service, http.MethodPost, url.Values{
		"username": {"admin"},
		"password": {"correct horse battery staple"},
	})
	if rec.Code != http.StatusSeeOther ||
		!strings.Contains(rec.Header().Get("Location"), "error=wizard") {
		t.Fatalf("setup = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
