package servergroup_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/servergroup"
)

func listeningServer(t *testing.T) servergroup.NamedServer {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return servergroup.NamedServer{
		Name: "test",
		Server: &http.Server{
			Addr:              addr,
			Handler:           http.NewServeMux(),
			ReadHeaderTimeout: time.Second,
		},
	}
}

func TestContextCancelShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- servergroup.Run(ctx, time.Second, []servergroup.NamedServer{listeningServer(t)})
	}()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestFirstWorkerReturnShutsDownServers(t *testing.T) {
	server := listeningServer(t)
	worker := func(context.Context) error { return nil }

	done := make(chan error, 1)
	go func() {
		done <- servergroup.Run(context.Background(), time.Second,
			[]servergroup.NamedServer{server}, worker)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker return did not shut down the group")
	}
}

func TestWorkerErrorPropagates(t *testing.T) {
	wantErr := errors.New("worker failed")
	blocking := func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}
	failing := func(context.Context) error { return wantErr }

	err := servergroup.Run(context.Background(), time.Second,
		[]servergroup.NamedServer{listeningServer(t)}, blocking, failing)
	if !errors.Is(err, wantErr) {
		t.Errorf("Run = %v, want %v", err, wantErr)
	}
}

func TestServerBindErrorPropagates(t *testing.T) {
	holder, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = holder.Close() }()

	clash := servergroup.NamedServer{
		Name: "clash",
		Server: &http.Server{
			Addr:              holder.Addr().String(),
			ReadHeaderTimeout: time.Second,
		},
	}
	if err := servergroup.Run(context.Background(), time.Second,
		[]servergroup.NamedServer{clash}); err == nil {
		t.Error("Run = nil, want bind error")
	}
}
