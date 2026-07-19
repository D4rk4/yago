package yagonode

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

func failingInterfaceAddrs() ([]net.Addr, error) {
	return nil, errors.New("no interfaces")
}

func TestBindingSourceHandlesInterfaceAndStoreErrors(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	src := newBindingSource(store, testConfig(t), nil)
	src.interfaces = failingInterfaceAddrs
	ctx := context.Background()

	if view := src.Bindings(ctx); view.Error != hostInterfacesUnavailable || len(view.Items) != 0 {
		t.Fatalf("Bindings should report unavailable interfaces: %+v", view)
	}

	// With interface discovery failing, only the empty host counts as local, so a
	// blank-host change validates; the nil recorder exercises the no-op record path.
	res, err := src.UpdateBinding(ctx, adminui.BindChange{
		Key: bindKeyPeer, Host: "", Port: "8080",
	})
	if err != nil || !res.OK {
		t.Fatalf("UpdateBinding = %+v err=%v", res, err)
	}

	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := src.UpdateBinding(ctx, adminui.BindChange{
		Key: bindKeyPeer, Host: "", Port: "8080",
	}); err == nil {
		t.Fatal("UpdateBinding should surface the store error")
	}
	if _, err := src.UpdateBinding(ctx, adminui.BindChange{
		Key: bindKeyPublic, Action: adminui.BindActionDisable,
	}); err == nil {
		t.Fatal("Disable should surface the store error")
	}
	if _, err := src.UpdateBinding(ctx, adminui.BindChange{
		Key: bindKeyOps, Action: adminui.BindActionReset,
	}); err == nil {
		t.Fatal("Reset should surface the store error")
	}
}
