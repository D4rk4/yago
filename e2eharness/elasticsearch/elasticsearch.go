//go:build e2e

// Package elasticsearch starts a disposable Elasticsearch container for e2e suites.
package elasticsearch

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
	Image = "docker.elastic.co/elasticsearch/elasticsearch:8.15.3"
	Port  = "9200/tcp"
)

func Start(t *testing.T, ctx context.Context, networkName, alias string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        Image,
			ExposedPorts: []string{Port},
			Env: map[string]string{
				"discovery.type":         "single-node",
				"xpack.security.enabled": "false",
				"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
			},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {alias}},
			WaitingFor: wait.ForHTTP("/_cluster/health").
				WithPort(Port).
				WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start elasticsearch container %s: %v", Image, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "elasticsearch", container)
	return containerurl.HostURL(t, ctx, container, Port)
}

func NetworkURL(alias string) string {
	return "http://" + alias + ":9200"
}
