package yagonode

import (
	"context"
	"net"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

func fakeBindInterfaces() ([]net.Addr, error) {
	return []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1")},
		&net.IPNet{IP: net.ParseIP("192.168.1.5")},
	}, nil
}

func newTestBindingSource(
	t *testing.T,
	envConfig nodeConfig,
) (*bindingSource, *settingsstore.Store, *events.Recorder) {
	t.Helper()
	if envConfig.PeerAddr == "" {
		envConfig.PeerAddr = ":8090"
	}
	if envConfig.OpsAddr == "" {
		envConfig.OpsAddr = ":9090"
	}

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	recorder := events.NewRecorder(events.DefaultCapacity)

	source := newBindingSource(store, envConfig, recorder)
	source.interfaces = fakeBindInterfaces

	return source, store, recorder
}

func peerBindItem(t *testing.T, view adminui.BindingsView) adminui.BindItem {
	t.Helper()

	for _, item := range view.Items {
		if item.Key == bindKeyPeer {
			return item
		}
	}
	t.Fatal("peer bind item not present")

	return adminui.BindItem{}
}

func TestBindingSourceReportsEnvironmentAddress(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090", OpsAddr: ":9090"})

	item := peerBindItem(t, source.Bindings(context.Background()))
	if item.Host != "" || item.Port != "8090" {
		t.Fatalf("item = (%q,%q), want (\"\",\"8090\")", item.Host, item.Port)
	}
	if !item.ListenerEnabled {
		t.Fatal("configured peer listener reported disabled")
	}
	if item.Overridden {
		t.Fatal("no override stored, item should not be overridden")
	}
	if len(item.Interfaces) == 0 {
		t.Fatal("no interface options offered")
	}
}

func TestBindingSourceReportsDisabledOptionalListener(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090", OpsAddr: ":9090"})
	view := source.Bindings(context.Background())
	for _, item := range view.Items {
		if item.Key != bindKeyPublic {
			continue
		}
		if item.ListenerEnabled || item.Host != "" || item.Port != "" {
			t.Fatalf("disabled public listener = %+v", item)
		}

		return
	}
	t.Fatal("public bind item not present")
}

func TestBindingSourceReportsStoredOverride(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090"})
	if err := store.Set(context.Background(), bindKeyPeer, "127.0.0.1:8091"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	item := peerBindItem(t, source.Bindings(context.Background()))
	if item.Host != "127.0.0.1" || item.Port != "8091" || !item.Overridden {
		t.Fatalf(
			"item = (%q,%q,overridden=%v), want (127.0.0.1,8091,true)",
			item.Host,
			item.Port,
			item.Overridden,
		)
	}
}

func TestBindingSourceReportsUnavailableStoredState(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := source.Bindings(ctx)
	if view.Error != storedBindingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("cancelled read = %+v", view)
	}

	if err := store.Set(context.Background(), bindKeyPeer, "invalid"); err != nil {
		t.Fatalf("Set invalid bind: %v", err)
	}
	view = source.Bindings(context.Background())
	if view.Error != storedBindingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("invalid bind = %+v", view)
	}
}

func TestBindingSourceReportsInvalidConfiguredAddress(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestBindingSource(t, nodeConfig{PeerAddr: "invalid"})
	view := source.Bindings(context.Background())
	if view.Error != storedBindingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("invalid configured bind = %+v", view)
	}
}

func TestBindingSourceUpdatePersistsAndRecords(t *testing.T) {
	t.Parallel()

	source, store, recorder := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090"})
	ctx := context.Background()

	result, err := source.UpdateBinding(
		ctx,
		adminui.BindChange{Key: bindKeyPeer, Host: "127.0.0.1", Port: "8091"},
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if !result.OK {
		t.Fatalf("result = %+v, want OK", result)
	}

	stored, set, err := store.Get(ctx, bindKeyPeer)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !set || stored != "127.0.0.1:8091" {
		t.Fatalf("stored = (%q,%v), want (127.0.0.1:8091,true)", stored, set)
	}
	if recent := recorder.Recent(
		1,
	); len(recent) != 1 ||
		recent[0].Category != events.CategoryConfig {
		t.Fatalf("expected one config event, got %+v", recent)
	}
}

func TestBindingSourceUpdateAcceptsAllInterfaces(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090"})

	result, err := source.UpdateBinding(
		context.Background(),
		adminui.BindChange{Key: bindKeyPeer, Host: "", Port: "8099"},
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if !result.OK {
		t.Fatalf("all-interfaces bind rejected: %+v", result)
	}
}

func TestBindingSourceUpdateRejectsForeignHost(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{OpsAddr: ":9090"})
	ctx := context.Background()

	result, err := source.UpdateBinding(
		ctx,
		adminui.BindChange{Key: bindKeyOps, Host: "10.0.0.99", Port: "9090"},
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if result.OK {
		t.Fatal("bind to a non-local address accepted; lockout guardrail failed")
	}
	if _, set, _ := store.Get(ctx, bindKeyOps); set {
		t.Fatal("rejected bind was stored")
	}
}

func TestBindingSourceUpdateRejectsBadPort(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090"})

	result, err := source.UpdateBinding(
		context.Background(),
		adminui.BindChange{Key: bindKeyPeer, Host: "127.0.0.1", Port: "0"},
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if result.OK {
		t.Fatal("out-of-range port accepted")
	}
}

func TestBindingSourceUpdateRejectsUnknownSurface(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestBindingSource(t, nodeConfig{})

	result, err := source.UpdateBinding(
		context.Background(),
		adminui.BindChange{Key: "bind.nope", Host: "127.0.0.1", Port: "80"},
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if result.OK {
		t.Fatal("unknown surface accepted")
	}
}
