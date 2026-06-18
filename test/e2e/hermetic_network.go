//go:build e2e

package e2e

import (
	"context"
	"net"
	"testing"

	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
)

func newHermeticNetwork(t *testing.T, ctx context.Context) *testcontainers.DockerNetwork {
	t.Helper()
	noMasquerade := tcnetwork.CustomizeNetworkOption(func(req *dockernetwork.CreateOptions) error {
		if req.Options == nil {
			req.Options = map[string]string{}
		}
		req.Options["com.docker.network.bridge.enable_ip_masquerade"] = "false"
		return nil
	})
	nw, err := tcnetwork.New(ctx, tcnetwork.WithDriver("bridge"), noMasquerade)
	if err != nil {
		t.Fatalf("create hermetic docker network: %v", err)
	}
	t.Cleanup(func() { _ = nw.Remove(context.Background()) })
	return nw
}

func hostURL(t *testing.T, ctx context.Context, c testcontainers.Container) string {
	t.Helper()
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("resolve container host: %v", err)
	}
	port, err := c.MappedPort(ctx, httpPort)
	if err != nil {
		t.Fatalf("resolve mapped port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, port.Port())
}
