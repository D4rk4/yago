//go:build e2e

package e2e

import (
	"context"
	"testing"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
)

const nodeMemoryLimitBytes int64 = 4 << 30

func constrainNodeMemory(hostConfig *dockercontainer.HostConfig) {
	hostConfig.Memory = nodeMemoryLimitBytes
	hostConfig.MemorySwap = nodeMemoryLimitBytes
}

func requireNodeMemoryLimit(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
) {
	t.Helper()
	inspection, err := container.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect node memory limit: %v", err)
	}
	if inspection.HostConfig == nil ||
		inspection.HostConfig.Memory != nodeMemoryLimitBytes ||
		inspection.HostConfig.MemorySwap != nodeMemoryLimitBytes {
		t.Fatalf(
			"node memory limit = %#v, want memory and swap %d",
			inspection.HostConfig,
			nodeMemoryLimitBytes,
		)
	}
}
