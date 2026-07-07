package landiscovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type greetCall struct {
	host string
	port int
	hash string
}

func packet(t *testing.T, announcement Announcement) []byte {
	t.Helper()
	raw, err := json.Marshal(announcement)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return raw
}

func TestBeaconGreetsValidAnnouncements(t *testing.T) {
	var calls []greetCall
	beacon := New(
		"freeworld",
		"self-hash",
		8090,
		func(_ context.Context, host string, port int, hash string) {
			calls = append(calls, greetCall{host: host, port: port, hash: hash})
		},
	)

	beacon.HandlePacket(context.Background(), packet(t, Announcement{
		Magic: beaconMagic, Network: "freeworld", Hash: "peer-hash", Port: 8090,
	}), "192.168.1.42")

	if len(calls) != 1 ||
		calls[0] != (greetCall{host: "192.168.1.42", port: 8090, hash: "peer-hash"}) {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestBeaconIgnoresInvalidAnnouncements(t *testing.T) {
	calls := 0
	beacon := New("freeworld", "self-hash", 8090, func(context.Context, string, int, string) {
		calls++
	})

	cases := map[string][]byte{
		"garbage": []byte("not json"),
		"wrong magic": packet(
			t,
			Announcement{Magic: "other", Network: "freeworld", Hash: "p", Port: 1},
		),
		"wrong network": packet(
			t,
			Announcement{Magic: beaconMagic, Network: "othernet", Hash: "p", Port: 1},
		),
		"own hash": packet(
			t,
			Announcement{Magic: beaconMagic, Network: "freeworld", Hash: "self-hash", Port: 1},
		),
		"empty hash": packet(t, Announcement{Magic: beaconMagic, Network: "freeworld", Port: 1}),
		"bad port": packet(
			t,
			Announcement{Magic: beaconMagic, Network: "freeworld", Hash: "p", Port: 0},
		),
		"huge port": packet(
			t,
			Announcement{Magic: beaconMagic, Network: "freeworld", Hash: "p", Port: 70000},
		),
	}
	for name, raw := range cases {
		beacon.HandlePacket(context.Background(), raw, "192.168.1.42")
		if calls != 0 {
			t.Fatalf("%s: greeted an invalid announcement", name)
		}
	}
}

func TestBeaconRateLimitsRepeatAnnouncements(t *testing.T) {
	calls := 0
	beacon := New("freeworld", "self-hash", 8090, func(context.Context, string, int, string) {
		calls++
	})
	now := time.Unix(1_700_000_000, 0)
	beacon.now = func() time.Time { return now }

	announce := packet(t, Announcement{
		Magic: beaconMagic, Network: "freeworld", Hash: "peer-hash", Port: 8090,
	})
	beacon.HandlePacket(context.Background(), announce, "192.168.1.42")
	beacon.HandlePacket(context.Background(), announce, "192.168.1.42")
	if calls != 1 {
		t.Fatalf("calls = %d, want the repeat suppressed", calls)
	}

	now = now.Add(regreetWindow + time.Second)
	beacon.HandlePacket(context.Background(), announce, "192.168.1.42")
	if calls != 2 {
		t.Fatalf("calls = %d, want a fresh greet after the window", calls)
	}

	beacon.HandlePacket(context.Background(), announce, "192.168.1.77")
	if calls != 3 {
		t.Fatalf("calls = %d, a different address is always fresh", calls)
	}
}

func TestBeaconDisabledConstructions(t *testing.T) {
	if New("net", "", 8090, func(context.Context, string, int, string) {}) != nil {
		t.Fatal("empty hash must disable the beacon")
	}
	if New("net", "hash", 0, func(context.Context, string, int, string) {}) != nil {
		t.Fatal("missing port must disable the beacon")
	}
	if New("net", "hash", 8090, nil) != nil {
		t.Fatal("nil greeter must disable the beacon")
	}
	var beacon *Beacon
	done := make(chan struct{})
	go func() { beacon.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nil beacon Run must return immediately")
	}
}
