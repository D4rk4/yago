package yagonode

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagoproto"
)

func TestAssembledNodeFollowsSeedlistRedirectAndRefusesHelloRedirect(t *testing.T) {
	var seedlistReads, helloAttempts, helloDestinationHits atomic.Int64
	helloAttempted := make(chan struct{}, 1)
	listed := helloRosterSeed(t, "redirected-seedlist-peer", "192.0.2.10")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/seedlist":
			http.Redirect(w, r, "/seedlist-moved", http.StatusMovedPermanently)
		case "/seedlist-moved":
			seedlistReads.Add(1)
			seedlist := yagomodel.EncodeCompactWireForm(listed.String()) + "\n"
			_, _ = strings.NewReader(seedlist).WriteTo(w)
		case yagoproto.PathHello:
			helloAttempts.Add(1)
			select {
			case helloAttempted <- struct{}{}:
			default:
			}
			http.Redirect(w, r, "/hello-moved", http.StatusFound)
		default:
			helloDestinationHits.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	config := testConfig(t)
	config.SeedlistURLs = []string{"http://seeds.example.test/seedlist"}
	assembled, err := assembleNode(
		t.Context(),
		config,
		openTestVault(t),
		clientDialingOnly(server),
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	announced := make(chan struct{})
	go func() {
		defer close(announced)
		assembled.announcer.Run(ctx)
	}()
	select {
	case <-helloAttempted:
	case <-time.After(10 * time.Second):
	}
	cancel()
	<-announced
	if got := seedlistReads.Load(); got == 0 {
		t.Fatal("redirected seedlist was not read")
	}
	if got := helloAttempts.Load(); got == 0 {
		t.Fatal("listed peer was never greeted")
	}

	assembled.announcer.GreetDiscovered(t.Context(), listed)
	if got := helloDestinationHits.Load(); got != 0 {
		t.Fatalf("hello followed redirect: destination hits = %d", got)
	}
}

func clientDialingOnly(server *httptest.Server) *http.Client {
	var dialer net.Dialer

	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}}
}
