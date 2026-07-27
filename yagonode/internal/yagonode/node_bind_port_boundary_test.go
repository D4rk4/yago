package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

// The listener port range is pinned only where it refuses, with ports well
// inside it standing in for the accepting side. A range that quietly excluded
// its own endpoints would keep every one of those tests green while refusing an
// operator the first and last port the machine can actually bind, so both edges
// are admitted here and both first values outside are turned away.
func TestBindPortRangeAdmitsItsOwnEndpoints(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  string
		want int
	}{
		{name: "lowest bindable port", raw: "1", want: 1},
		{name: "highest bindable port", raw: "65535", want: 65535},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			port, err := parseBindPort(test.raw)
			if err != nil || port != test.want {
				t.Fatalf("parseBindPort(%q) = %d, %v", test.raw, port, err)
			}
		})
	}
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "one below the range", raw: "0"},
		{name: "one above the range", raw: "65536"},
		{name: "negative", raw: "-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if port, err := parseBindPort(test.raw); err == nil {
				t.Fatalf("parseBindPort(%q) = %d, want a refusal", test.raw, port)
			}
		})
	}
}

// The console shares that range, and its refusal has to reach the operator as a
// refusal rather than as a stored override the node then fails to bind at the
// next restart.
func TestBindingSourceAdmitsTheEndpointsAndRefusesPastThem(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{PeerAddr: ":8090"})
	result, err := source.UpdateBinding(t.Context(), adminui.BindChange{
		Key: bindKeyPeer, Host: "127.0.0.1", Port: "65535",
	})
	if err != nil || !result.OK {
		t.Fatalf("highest port = %+v, err = %v", result, err)
	}
	if stored, set, err := store.Get(t.Context(), bindKeyPeer); err != nil || !set ||
		stored != "127.0.0.1:65535" {
		t.Fatalf("stored bind = (%q,%v), err = %v", stored, set, err)
	}
	result, err = source.UpdateBinding(t.Context(), adminui.BindChange{
		Key: bindKeyPeer, Host: "127.0.0.1", Port: "65536",
	})
	if err != nil || result.OK || result.Message != bindingInvalidAddressMessage {
		t.Fatalf("port past the range = %+v, err = %v", result, err)
	}
	if stored, _, err := store.Get(t.Context(), bindKeyPeer); err != nil ||
		stored != "127.0.0.1:65535" {
		t.Fatalf("refused port replaced the stored bind with %q, err = %v", stored, err)
	}
}
