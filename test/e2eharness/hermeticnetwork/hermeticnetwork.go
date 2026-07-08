//go:build e2e

package hermeticnetwork

import (
	"context"
	"testing"

	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
)

func New(t *testing.T, ctx context.Context) *testcontainers.DockerNetwork {
	t.Helper()
	noMasquerade := tcnetwork.CustomizeNetworkOption(func(req *dockernetwork.CreateOptions) error {
		if req.Options == nil {
			req.Options = map[string]string{}
		}
		req.Options["com.docker.network.bridge.enable_ip_masquerade"] = "false"
		return nil
	})
	network, err := tcnetwork.New(ctx, tcnetwork.WithDriver("bridge"), noMasquerade)
	if err != nil {
		t.Fatalf("create hermetic docker network: %v", err)
	}
	t.Cleanup(func() { _ = network.Remove(context.Background()) })
	return network
}
