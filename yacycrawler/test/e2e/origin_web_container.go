//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
)

const (
	originImage = "docker.io/library/nginx:alpine"
	originAlias = "origin"
	originPage  = `<html lang="en"><title>Hi</title><body>words here</body></html>`
)

func startOrigin(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          originImage,
			ExposedPorts:   []string{"80/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {originAlias}},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(originPage),
				ContainerFilePath: "/usr/share/nginx/html/index.html",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForHTTP("/").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start origin container %s: %v", originImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "origin", container)
	return "http://" + originAlias + "/"
}
