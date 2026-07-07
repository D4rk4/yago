//go:build e2e

package dockernetwork

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
)

func New(t *testing.T, ctx context.Context) *testcontainers.DockerNetwork {
	t.Helper()
	network, err := tcnetwork.New(ctx)
	if err != nil {
		t.Fatalf("create docker network: %v", err)
	}
	t.Cleanup(func() { _ = network.Remove(context.Background()) })
	return network
}
