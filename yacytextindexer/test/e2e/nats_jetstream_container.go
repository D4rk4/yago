//go:build e2e

package e2e

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	natsImage = "docker.io/library/nats:2.10-alpine"
	natsAlias = "nats"
	natsPort  = "4222/tcp"
)

func startNATS(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          natsImage,
			Cmd:            []string{"--jetstream"},
			ExposedPorts:   []string{natsPort},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {natsAlias}},
			WaitingFor:     wait.ForLog("Server is ready").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start NATS container %s: %v", natsImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "nats", container)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve NATS host: %v", err)
	}
	port, err := container.MappedPort(ctx, natsPort)
	if err != nil {
		t.Fatalf("resolve NATS mapped port: %v", err)
	}
	return "nats://" + net.JoinHostPort(host, port.Port())
}

func natsNetworkURL() string {
	return "nats://" + natsAlias + ":4222"
}
