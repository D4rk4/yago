//go:build e2e

// Package manticore starts a disposable Manticore container for e2e suites.
package manticore

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerurl"
)

const (
	Image = "manticoresearch/manticore:27.1.5"
	Port  = "9308/tcp"
)

func Start(t *testing.T, ctx context.Context, networkName, alias string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          Image,
			ExposedPorts:   []string{Port},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {alias}},
			WaitingFor: wait.ForListeningPort(Port).
				WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start manticore container %s: %v", Image, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "manticore", container)
	hostURL := containerurl.HostURL(t, ctx, container, Port)
	awaitWritePathReady(t, ctx, hostURL)
	return hostURL
}

func NetworkURL(alias string) string {
	return "http://" + alias + ":9308"
}
