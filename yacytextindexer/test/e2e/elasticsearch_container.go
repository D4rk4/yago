//go:build e2e

package e2e

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	elasticsearchImage = "docker.elastic.co/elasticsearch/elasticsearch:8.15.3"
	elasticsearchAlias = "elasticsearch"
	elasticsearchPort  = "9200/tcp"
	elasticsearchIndex = "yacy-text"
)

func startElasticsearch(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        elasticsearchImage,
			ExposedPorts: []string{elasticsearchPort},
			Env: map[string]string{
				"discovery.type":         "single-node",
				"xpack.security.enabled": "false",
				"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
			},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {elasticsearchAlias}},
			WaitingFor: wait.ForHTTP("/_cluster/health").
				WithPort(elasticsearchPort).
				WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start elasticsearch container %s: %v", elasticsearchImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "elasticsearch", container)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve elasticsearch host: %v", err)
	}
	port, err := container.MappedPort(ctx, elasticsearchPort)
	if err != nil {
		t.Fatalf("resolve elasticsearch mapped port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, port.Port())
}

func elasticsearchNetworkURL() string {
	return "http://" + elasticsearchAlias + ":9200"
}
