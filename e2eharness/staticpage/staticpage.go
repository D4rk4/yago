//go:build e2e

package staticpage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
)

const image = "docker.io/library/nginx:alpine"

func Start(t *testing.T, ctx context.Context, networkName, alias, html string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          image,
			ExposedPorts:   []string{"80/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {alias}},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(html),
				ContainerFilePath: "/usr/share/nginx/html/index.html",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForHTTP("/").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start static page container %s: %v", image, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, alias, container)
	return "http://" + alias + "/"
}
