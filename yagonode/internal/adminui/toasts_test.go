package adminui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestConfigUpdateRendersSuccessToast(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view:   portalSettingsView(true),
		result: SettingsResult{OK: true, Message: "Saved."},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":   {"portal.enabled"},
		"value": {"false"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `class="cds-toasts"`) ||
		!strings.Contains(got.body, "cds-toast--success") {
		t.Fatalf("success toast not rendered:\n%s", got.body)
	}
}

func TestSecurityRejectionRendersErrorToast(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:     securityViewWithKey(),
		pwResult: PasswordChangeResult{OK: false, Message: "Rejected."},
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form":    {"password"},
		"new":     {"a"},
		"confirm": {"b"},
	})
	if !strings.Contains(got.body, "cds-toast--error") {
		t.Fatalf("error toast not rendered:\n%s", got.body)
	}
}
