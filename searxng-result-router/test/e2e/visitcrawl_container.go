//go:build e2e

package e2e

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
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
				"NATS_URL":  natsNetworkURL(),
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
	dumpLogsOnFailure(t, "visitcrawl", container)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve visitcrawl host: %v", err)
	}
	port, err := container.MappedPort(ctx, visitcrawlPort)
	if err != nil {
		t.Fatalf("resolve visitcrawl mapped port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, port.Port())
}

func visitcrawlImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv(envVisitcrawlImage)
	if image == "" {
		t.Fatalf(
			"%s is not set; build the visitcrawl image first (run via `make e2e-plugin`)",
			envVisitcrawlImage,
		)
	}
	return image
}
