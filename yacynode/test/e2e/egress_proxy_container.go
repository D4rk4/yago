//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	egressProxyImage = "pretix/smokescreen:latest"
	egressProxyAlias = "smokescreen"
	egressProxyPort  = "4750"
)

func startEgressProxy(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image: egressProxyImage,
			Cmd: []string{
				"--listen-ip", "0.0.0.0",
				"--allow-range", "10.0.0.0/8",
				"--allow-range", "172.16.0.0/12",
				"--allow-range", "192.168.0.0/16",
			},
			ExposedPorts:   []string{egressProxyPort + "/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {egressProxyAlias}},
			WaitingFor: wait.ForListeningPort(egressProxyPort + "/tcp").
				WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start egress proxy container %s: %v", egressProxyImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "smokescreen", container)
}

func egressProxyNetworkURL() string {
	return "http://" + egressProxyAlias + ":" + egressProxyPort
}
