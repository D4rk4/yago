//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// startCrawlerForNode runs the crawler container pointed at the real node's crawl
// RPC listener over the shared network, rather than at an in-process double.
func startCrawlerForNode(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          requireImage(t, envCrawlerImage, "crawler"),
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {crawlerAlias}},
			Env: map[string]string{
				"YACYCRAWLER_NODE_RPC_ADDR":          nodeAlias + ":9091",
				"YACYCRAWLER_ALLOW_PRIVATE_NETWORKS": "true",
				"YACYCRAWLER_WORKERS":                "1",
				"LOG_LEVEL":                          "debug",
			},
		},
	})
	if err != nil {
		t.Fatalf("start crawler container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "crawler-node", container)
}
