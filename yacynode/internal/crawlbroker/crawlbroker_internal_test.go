package crawlbroker

import (
	"errors"
	"net"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

func TestOpenServesAndCloses(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if broker.Orders == nil || broker.Ingest == nil {
		t.Fatal("broker ports must be wired")
	}
	broker.Close()
}

func TestOpenReturnsListenError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	restore := listenCrawlRPC
	t.Cleanup(func() { listenCrawlRPC = restore })
	listenCrawlRPC = func(string) (net.Listener, error) { return nil, errors.New("listen failed") }

	if _, err := Open(Config{ListenAddr: ":0"}, v); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestOpenReturnsQueueError(t *testing.T) {
	engine := newScriptedEngine()
	engine.provisionErrors[orderBucket] = errors.New("provision failed")
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v); err == nil {
		t.Fatal("expected queue registration error")
	}
}
