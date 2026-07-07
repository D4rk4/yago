//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/egressproxy"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/natsjetstream"
)

const (
	crawlerAlias    = "crawler"
	envCrawlerImage = "YACYCRAWLER_IMAGE"
)

func startCrawler(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          crawlerImage(t),
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {crawlerAlias}},
			Env: map[string]string{
				"NATS_URL":                      natsjetstream.NetworkURL(),
				"YACYCRAWLER_PROXY_URL":         egressproxy.NetworkURL(),
				"YACYCRAWLER_FETCH_CONCURRENCY": "1",
				"LOG_LEVEL":                     "debug",
			},
		},
	})
	if err != nil {
		t.Fatalf("start crawler container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "crawler", container)
}

func crawlerImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv(envCrawlerImage)
	if image == "" {
		t.Fatalf(
			"%s is not set; build the crawler image first (run via `make e2e`)",
			envCrawlerImage,
		)
	}
	return image
}
