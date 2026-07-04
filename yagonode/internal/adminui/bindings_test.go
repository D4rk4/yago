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
		Key:   "bind.peer",
		Title: "Peer protocol (P2P)",
		Host:  "",
		Port:  "8090",
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
		`name="port"`, `value="binding"`, "127.0.0.1 (loopback)",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("bind editor missing %q", want)
		}
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
		binding.change.Port != "8091" {
		t.Fatalf("unexpected change %+v", binding.change)
	}
	if !strings.Contains(got.body, "after the next restart") {
		t.Fatal("bind result message not shown")
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
