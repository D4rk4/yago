//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerurl"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/natsjetstream"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/requiredimage"
)

const (
	visitcrawlAlias    = "visitcrawl"
	visitcrawlPort     = "8091/tcp"
	envVisitcrawlImage = "YACYVISITCRAWL_IMAGE"
)

func startVisitcrawl(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          visitcrawlImage(t),
			ExposedPorts:   []string{visitcrawlPort},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {visitcrawlAlias}},
			Env: map[string]string{
				"NATS_URL":  natsjetstream.NetworkURL(),
				"LOG_LEVEL": "debug",
			},
			WaitingFor: wait.ForHTTP("/visit").
				WithPort(visitcrawlPort).
				WithStatusCodeMatcher(func(status int) bool { return status == 400 }).
				WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start visitcrawl container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "visitcrawl", container)
	return containerurl.HostURL(t, ctx, container, visitcrawlPort)
}

func visitcrawlImage(t *testing.T) string {
	t.Helper()
	return requiredimage.FromEnv(t, envVisitcrawlImage, "visitcrawl", "e2e-plugin")
}
