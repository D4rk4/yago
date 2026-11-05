package peerroster

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestRepeatedAnsweredContactWritesNoAnsweredEndpointRow(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	if engine.puts[answeredEndpointsBucket] != 1 {
		t.Fatalf("first answered contact puts = %d, want 1", engine.puts[answeredEndpointsBucket])
	}
	updates := engine.updates.Load()

	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	r.ObserveAnsweredCaller(
		t.Context(), peer, yagomodel.PeerSenior, answeredOrigin(t, "http://203.0.113.1:8090"),
	)

	if engine.puts[answeredEndpointsBucket] != 1 {
		t.Fatalf(
			"repeated answered contact puts = %d, want 1",
			engine.puts[answeredEndpointsBucket],
		)
	}
	if got := engine.updates.Load() - updates; got != 2 {
		t.Fatalf("repeated answered contacts committed %d updates, want 2", got)
	}
}

func TestChangedAnsweredContactRewritesAnsweredEndpointRow(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	alternate, err := yagomodel.ParseHost("198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	peer.IP6 = yagomodel.Some([]yagomodel.Host{alternate})
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")

	answered := answeredOrigin(t, "http://198.51.100.7:8090")
	r.ObserveAnsweredResponder(t.Context(), peer, answered)
	if engine.puts[answeredEndpointsBucket] != 2 {
		t.Fatalf("changed endpoint puts = %d, want 2", engine.puts[answeredEndpointsBucket])
	}
	assertAnsweredEndpoint(t, r, peer.Hash, answered)

	moved := peer.Copy()
	moved.Port = yagomodel.Some(yagomodel.Port(8091))
	r.ObserveAnsweredResponder(t.Context(), moved, answeredOrigin(t, "http://198.51.100.7:8091"))
	if engine.puts[answeredEndpointsBucket] != 3 {
		t.Fatalf("changed identity puts = %d, want 3", engine.puts[answeredEndpointsBucket])
	}
	assertAnsweredEndpoint(t, r, peer.Hash, answeredOrigin(t, "http://198.51.100.7:8091"))
}

func TestAnsweredContactRewritesCorruptAnsweredEndpointRow(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	engine.buckets[answeredEndpointsBucket][string(r.key(peer.Hash))] = []byte("bad")

	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")

	if engine.puts[answeredEndpointsBucket] != 2 {
		t.Fatalf("corrupt row rewrite puts = %d, want 2", engine.puts[answeredEndpointsBucket])
	}
	assertAnsweredEndpoint(t, r, peer.Hash, answeredOrigin(t, "http://203.0.113.1:8090"))
}

func TestCorruptAnsweredEndpointRowIsIgnoredWithWarning(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	corrupt := []byte("YPE1-corrupt-203.0.113.1")
	engine.buckets[answeredEndpointsBucket][string(r.key(peer.Hash))] = corrupt
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	assertNoAnsweredEndpoint(t, r, peer.Hash)

	logged := output.String()
	if !strings.Contains(logged, `"level":"WARN"`) ||
		!strings.Contains(logged, `"msg":"`+peerAnsweredEndpointUnreadableMessage+`"`) ||
		!strings.Contains(logged, `"peer":"`+peer.Hash.String()+`"`) {
		t.Fatalf("corrupt answered endpoint warning = %s", logged)
	}
	if strings.Contains(logged, "203.0.113.1") || strings.Contains(logged, "corrupt") {
		t.Fatalf("corrupt answered endpoint warning leaked row content: %s", logged)
	}
	if engine.buckets[answeredEndpointsBucket][string(r.key(peer.Hash))] == nil {
		t.Fatal("ignored corrupt answered endpoint row was removed")
	}

	output.Reset()
	r.ObserveResponder(t.Context(), peer)
	if strings.Contains(output.String(), peerAnsweredEndpointUnreadableMessage) {
		t.Fatalf("plain observation reported the answered endpoint row: %s", output.String())
	}
}
