//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

const (
	crawlerAlias    = "crawler"
	envCrawlerImage = "YAGO_CRAWLER_IMAGE"
	hostInternal    = "host.testcontainers.internal"
)

func startCrawler(t *testing.T, ctx context.Context, networkName string, exchangePort int) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:           crawlerImage(t),
			Networks:        []string{networkName},
			NetworkAliases:  map[string][]string{networkName: {crawlerAlias}},
			HostAccessPorts: []int{exchangePort},
			Env: map[string]string{
				"YAGO_CRAWLER_NODE_RPC_ADDR": fmt.Sprintf(
					"%s:%d",
					hostInternal,
					exchangePort,
				),
				"YAGO_CRAWLER_ALLOW_PRIVATE_NETWORKS": "true",
				"YAGO_CRAWLER_WORKERS":                "1",
				"LOG_LEVEL":                           "debug",
			},
		},
	})
	if err != nil {
		t.Fatalf("start crawler container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "crawler", container)
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
