// Package landiscovery finds peers on the local network the way Syncthing's
// local discovery does: a small UDP beacon broadcast on a fixed port announces
// this node's hash and peer port, and received announcements from other nodes
// hand a candidate endpoint to the greeter, which runs the ordinary hello
// exchange — the beacon itself is never trusted for anything but «someone at
// this address claims to be a peer», so a forged packet can at most trigger
// one verified greet. DHT bootstrap needs a reachable seedlist; two nodes on
// one LAN behind the same NAT find each other with no infrastructure at all.
package landiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	// Port is the fixed UDP beacon port (one for send and receive, so every
	// node needs only one rule opened).
	Port = 21777
	// beaconMagic guards against unrelated UDP chatter on the port.
	beaconMagic = "yago-lan-v1"
	// announceInterval paces broadcasts; Syncthing announces every ~30-60s.
	announceInterval = 45 * time.Second
	// regreetWindow suppresses repeated greets of one address: LAN peers
	// announce continuously, and one verified hello per window is plenty.
	regreetWindow = 10 * time.Minute
	maxPacket     = 512
)

// Announcement is the beacon payload. Only the hash, network, and peer port
// travel; the peer's address is taken from the packet source, never from the
// payload, so a beacon cannot point the greeter at a third party.
type Announcement struct {
	Magic   string `json:"magic"`
	Network string `json:"network"`
	Hash    string `json:"hash"`
	Port    int    `json:"port"`
}

// Greeter runs the verified hello exchange against a discovered endpoint.
type Greeter func(ctx context.Context, host string, port int, hash string)

// Beacon broadcasts this node's announcement and greets announcers it hears.
type Beacon struct {
	network  string
	selfHash string
	peerPort int
	greet    Greeter
	interval time.Duration
	now      func() time.Time

	mu       sync.Mutex
	lastSeen map[string]time.Time
}

// New builds a beacon; a nil greeter or empty hash disables it (New returns
// nil and Run on a nil beacon is a no-op).
func New(network, selfHash string, peerPort int, greet Greeter) *Beacon {
	if greet == nil || selfHash == "" || peerPort <= 0 {
		return nil
	}

	return &Beacon{
		network:  network,
		selfHash: selfHash,
		peerPort: peerPort,
		greet:    greet,
		interval: announceInterval,
		now:      time.Now,
		lastSeen: map[string]time.Time{},
	}
}

// Run listens for announcements and broadcasts its own until ctx ends.
func (b *Beacon) Run(ctx context.Context) {
	if b == nil {
		return
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: Port})
	if err != nil {
		slog.WarnContext(ctx, "lan discovery disabled", slog.Any("error", err))

		return
	}
	defer func() { _ = conn.Close() }()
	go b.receiveLoop(ctx, conn)

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	b.broadcast(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.broadcast(ctx, conn)
		}
	}
}

func (b *Beacon) broadcast(ctx context.Context, conn *net.UDPConn) {
	payload, err := json.Marshal(Announcement{
		Magic:   beaconMagic,
		Network: b.network,
		Hash:    b.selfHash,
		Port:    b.peerPort,
	})
	if err != nil {
		return
	}
	target := &net.UDPAddr{IP: net.IPv4bcast, Port: Port}
	if _, err := conn.WriteToUDP(payload, target); err != nil {
		slog.DebugContext(ctx, "lan discovery broadcast failed", slog.Any("error", err))
	}
}

func (b *Beacon) receiveLoop(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, maxPacket)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return
		}
		n, from, err := conn.ReadFromUDP(buf)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			continue
		}
		b.HandlePacket(ctx, buf[:n], from.IP.String())
	}
}

// HandlePacket validates one received announcement and greets a fresh peer.
// It is exported for tests; the receive loop is its only production caller.
func (b *Beacon) HandlePacket(ctx context.Context, packet []byte, sourceHost string) {
	var announcement Announcement
	if err := json.Unmarshal(packet, &announcement); err != nil {
		return
	}
	if announcement.Magic != beaconMagic ||
		announcement.Network != b.network ||
		announcement.Hash == "" ||
		announcement.Hash == b.selfHash ||
		announcement.Port <= 0 || announcement.Port > 65535 {
		return
	}
	endpoint := fmt.Sprintf("%s:%d", sourceHost, announcement.Port)
	if !b.freshEndpoint(endpoint) {
		return
	}
	slog.InfoContext(ctx, "lan peer discovered",
		slog.String("peer", announcement.Hash),
		slog.String("endpoint", endpoint))
	b.greet(ctx, sourceHost, announcement.Port, announcement.Hash)
}

// freshEndpoint reports whether the endpoint has not been greeted within the
// re-greet window, recording it when fresh.
func (b *Beacon) freshEndpoint(endpoint string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if seen, ok := b.lastSeen[endpoint]; ok && b.now().Sub(seen) < regreetWindow {
		return false
	}
	b.lastSeen[endpoint] = b.now()

	return true
}
