package adminui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeBinding struct {
	view   BindingsView
	result BindResult
	err    error
	calls  int
	change BindChange
}

func (f *fakeBinding) Bindings(context.Context) BindingsView { return f.view }

func (f *fakeBinding) UpdateBinding(_ context.Context, change BindChange) (BindResult, error) {
	f.calls++
	f.change = change

	return f.result, f.err
}

func peerBindingView() BindingsView {
	return BindingsView{Items: []BindItem{{
		Key:             "bind.peer",
		Title:           "Peer protocol (P2P)",
		Host:            "",
		Port:            "8090",
		ListenerEnabled: true,
		Interfaces: []BindInterface{
			{Value: "", Label: "All interfaces"},
			{Value: "127.0.0.1", Label: "127.0.0.1 (loopback)"},
		},
	}}}
}

func TestConsoleConfigRendersBindEditor(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config:  fakeConfig{view: ConfigView{}},
		Binding: &fakeBinding{view: peerBindingView()},
	})
	got := do(t, console, "/admin/configuration")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Listen addresses", "Peer protocol (P2P)", `name="host"`,
		`name="port"`, `value="binding"`, `name="binding_action" value="set"`,
		`name="csrf_token"`, `action="/admin/configuration#panel-listen"`,
		`hx-boost="false"`, `data-allow-horizontal-overflow`, "127.0.0.1 (loopback)",
		"Current desired listener: all interfaces, port 8090.",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("bind editor missing %q", want)
		}
	}
}

func TestConsoleConfigLabelsDisabledOptionalListener(t *testing.T) {
	t.Parallel()

	view := BindingsView{Items: []BindItem{{
		Key:        "bind.public",
		Title:      "Public search",
		Interfaces: []BindInterface{{Value: "", Label: "All interfaces"}},
	}}}
	body := do(t, New(Options{
		Config:  fakeConfig{view: ConfigView{}},
		Binding: &fakeBinding{view: view},
	}), "/admin/configuration").body
	for _, want := range []string{
		"Public search",
		`<span class="cds-tag cds-tag--debug">disabled</span>`,
		"Current desired listener: disabled.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("disabled listener missing %q", want)
		}
	}
	if strings.Contains(body, "Current desired listener: all interfaces") {
		t.Fatal("disabled listener rendered as an active all-interface bind")
	}
}

func TestConsoleConfigBindUpdateApplies(t *testing.T) {
	t.Parallel()

	binding := &fakeBinding{
		view: peerBindingView(),
		result: BindResult{
			OK:      true,
			Message: "Peer protocol (P2P) will bind to 127.0.0.1:8091 after the next restart.",
		},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Binding: binding})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"form": {"binding"},
		"key":  {"bind.peer"},
		"host": {"127.0.0.1"},
		"port": {"8091"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if binding.calls != 1 {
		t.Fatalf("UpdateBinding called %d times, want 1", binding.calls)
	}
	if binding.change.Key != "bind.peer" || binding.change.Host != "127.0.0.1" ||
		binding.change.Port != "8091" || binding.change.Action != BindActionSet {
		t.Fatalf("unexpected change %+v", binding.change)
	}
	if !strings.Contains(got.body, "after the next restart") {
		t.Fatal("bind result message not shown")
	}
}

func TestConsoleConfigBindingActionsUseTheSameNoJavaScriptForm(t *testing.T) {
	t.Parallel()

	view := BindingsView{Items: []BindItem{{
		Key:             "bind.public",
		Title:           "Public search",
		Port:            "8080",
		ListenerEnabled: true,
		CanDisable:      true,
		Overridden:      true,
		Interfaces:      []BindInterface{{Value: "", Label: "All interfaces"}},
	}}}
	binding := &fakeBinding{view: view, result: BindResult{OK: true, Message: "Updated."}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Binding: binding})
	body := do(t, console, "/admin/configuration").body
	for _, want := range []string{
		`name="binding_action" value="set"`,
		`name="binding_action" value="disable" formnovalidate`,
		`name="binding_action" value="reset" formnovalidate`,
		`aria-label="Reset Public search to the environment address"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("binding form missing %q", want)
		}
	}

	for _, action := range []BindAction{BindActionDisable, BindActionReset} {
		got := doPost(t, console, "/admin/configuration", url.Values{
			"form":           {"binding"},
			"key":            {"bind.public"},
			"binding_action": {string(action)},
		})
		if got.status != http.StatusOK || binding.change.Action != action {
			t.Fatalf("action %q = status %d change %+v", action, got.status, binding.change)
		}
	}
}

func TestConsoleConfigBindingPreservesUnknownTypedAction(t *testing.T) {
	t.Parallel()

	binding := &fakeBinding{view: peerBindingView(), result: BindResult{
		Message: "Unknown binding action.",
	}}
	body := doPost(t, New(Options{
		Config: fakeConfig{view: ConfigView{}}, Binding: binding,
	}), "/admin/configuration", url.Values{
		"form":           {"binding"},
		"key":            {"bind.peer"},
		"binding_action": {"future-action"},
	}).body
	if binding.change.Action != BindAction("future-action") ||
		!strings.Contains(body, "Unknown binding action.") {
		t.Fatalf("unknown action = %+v body=%s", binding.change, body)
	}
}

func TestConsoleConfigBindUpdateWithoutSourceNotFound(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})
	got := doPost(t, console, "/admin/configuration", url.Values{
		"form": {"binding"},
		"key":  {"bind.peer"},
	})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}
