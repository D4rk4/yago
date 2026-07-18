//go:build e2e

package e2e

import (
	"context"
	"testing"

	dockerclient "github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

func crashContainer(t *testing.T, ctx context.Context, container testcontainers.Container) {
	t.Helper()
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Fatalf("open container provider: %v", err)
	}
	defer func() { _ = provider.Close() }()
	if _, err := provider.Client().ContainerKill(
		ctx,
		container.GetContainerID(),
		dockerclient.ContainerKillOptions{},
	); err != nil {
		t.Fatalf("kill container: %v", err)
	}
}
