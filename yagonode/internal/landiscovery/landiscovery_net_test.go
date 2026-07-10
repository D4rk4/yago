package landiscovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

var errMarshal = errors.New("marshal boom")

func init() {
	// Shorten the receive deadline so the timeout→continue and ctx-cancel
	// branches of receiveLoop run in milliseconds rather than a full second.
	// Set once here (never per-test) so no test writes it while a receiveLoop
	// goroutine is reading it — keeping the -race build clean.
	receiveReadTimeout = 20 * time.Millisecond
}

func newTestBeacon() *Beacon {
	return New("freeworld", "self-hash", 8090, func(context.Context, string, int, string) {})
}

func newLoopbackConn(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen loopback udp: %v", err)
	}

	return conn
}

func sendAnnouncement(t *testing.T, target net.Addr, announcement Announcement) {
	t.Helper()
	addr, ok := target.(*net.UDPAddr)
	if !ok {
		t.Fatalf("target %v is not a *net.UDPAddr", target)
	}
	sender, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer func() { _ = sender.Close() }()
	if _, err := sender.Write(packet(t, announcement)); err != nil {
		t.Fatalf("write announcement: %v", err)
	}
}

func TestBroadcastLogsWriteError(t *testing.T) {
	conn := newLoopbackConn(t)
	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	// A closed conn makes WriteToUDP fail, exercising the debug-log branch after
	// a successful marshal.
	newTestBeacon().broadcast(context.Background(), conn)
}

func TestBroadcastReturnsOnMarshalError(t *testing.T) {
	original := marshalAnnouncement
	t.Cleanup(func() { marshalAnnouncement = original })
	marshalAnnouncement = func(any) ([]byte, error) { return nil, errMarshal }

	conn := newLoopbackConn(t)
	t.Cleanup(func() { _ = conn.Close() })
	// Marshal fails, so broadcast returns before touching the conn.
	newTestBeacon().broadcast(context.Background(), conn)
}

func TestReceiveLoopReturnsOnDeadlineError(t *testing.T) {
	conn := newLoopbackConn(t)
	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	// SetReadDeadline on a closed conn errors, so receiveLoop returns at once.
	newTestBeacon().receiveLoop(context.Background(), conn)
}

func TestReceiveLoopHandlesAnnouncementThenContinues(t *testing.T) {
	greets := make(chan greetCall, 1)
	beacon := New("freeworld", "self-hash", 8090,
		func(_ context.Context, host string, port int, hash string) {
			greets <- greetCall{host: host, port: port, hash: hash}
		})
	conn := newLoopbackConn(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		beacon.receiveLoop(ctx, conn)
		close(done)
	}()

	sendAnnouncement(t, conn.LocalAddr(), Announcement{
		Magic: beaconMagic, Network: "freeworld", Hash: "peer-hash", Port: 8090,
	})

	select {
	case got := <-greets:
		want := greetCall{host: "127.0.0.1", port: 8090, hash: "peer-hash"}
		if got != want {
			t.Fatalf("greet = %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not greet the announcement")
	}

	time.Sleep(80 * time.Millisecond) // let reads time out → the continue branch
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not return after cancel")
	}
}

func TestRunDisablesWhenPortBusy(t *testing.T) {
	blocker, err := net.ListenUDP("udp4", &net.UDPAddr{Port: Port})
	if err != nil {
		t.Fatalf("pre-bind beacon port: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	done := make(chan struct{})
	go func() {
		newTestBeacon().Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run must return when the beacon port is busy")
	}
}

func TestRunBroadcastsUntilContextDone(t *testing.T) {
	beacon := newTestBeacon()
	beacon.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		beacon.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond) // allow the listen, receiveLoop, and a tick
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run must return after ctx cancel")
	}
}
