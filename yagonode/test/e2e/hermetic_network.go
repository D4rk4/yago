//go:build e2e

package e2e

import (
	"context"
	"net"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
)

func newHermeticNetwork(t *testing.T, ctx context.Context) *testcontainers.DockerNetwork {
	t.Helper()
	network, err := tcnetwork.New(
		ctx,
		tcnetwork.WithDriver("bridge"),
		withoutNetworkMasquerade(),
	)
	if err != nil {
		t.Fatalf("create hermetic docker network: %v", err)
	}
	t.Cleanup(func() { _ = network.Remove(context.Background()) })
	return network
}

func hostURL(t *testing.T, ctx context.Context, container testcontainers.Container) string {
	t.Helper()
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve container host: %v", err)
	}
	port, err := container.MappedPort(ctx, httpPort)
	if err != nil {
		t.Fatalf("resolve mapped port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, port.Port())
}

// nodePublicURL resolves the host URL for the node's dedicated public search
// listener, which carries the /yacysearch.* and portal surfaces that no longer
// share the peer port.
func nodePublicURL(t *testing.T, ctx context.Context, container testcontainers.Container) string {
	t.Helper()
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve container host: %v", err)
	}
	port, err := container.MappedPort(ctx, nodePublicHTTPPort)
	if err != nil {
		t.Fatalf("resolve mapped public port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, port.Port())
}
