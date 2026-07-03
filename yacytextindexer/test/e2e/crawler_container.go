//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
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
				"NATS_URL":                           natsNetworkURL(),
				"YACYCRAWLER_PROXY_URL":              egressProxyNetworkURL(),
				"YACYCRAWLER_WORKERS":                "1",
				"YACYCRAWLER_EXTRACTED_TEXT_ENABLED": "true",
				"NATS_EXTRACTED_TEXT_SUBJECT":        extractedTextSubject,
				"LOG_LEVEL":                          "debug",
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
