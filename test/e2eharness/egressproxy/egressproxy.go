//go:build e2e

package egressproxy

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
)

const (
	image = "pretix/smokescreen:latest"
	alias = "smokescreen"
	port  = "4750"
)

func Start(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image: image,
			Cmd: []string{
				"--listen-ip", "0.0.0.0",
				"--allow-range", "10.0.0.0/8",
				"--allow-range", "172.16.0.0/12",
				"--allow-range", "192.168.0.0/16",
			},
			ExposedPorts:   []string{port + "/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {alias}},
			WaitingFor: wait.ForListeningPort(port + "/tcp").
				WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start egress proxy container %s: %v", image, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "smokescreen", container)
}

func NetworkURL() string {
	return "http://" + alias + ":" + port
}
