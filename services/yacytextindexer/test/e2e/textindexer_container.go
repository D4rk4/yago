//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/natsjetstream"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/requiredimage"
)

const (
	textIndexerAlias    = "textindexer"
	envTextIndexerImage = "YACYTEXTINDEXER_IMAGE"
)

func startTextIndexer(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          textIndexerImage(t),
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {textIndexerAlias}},
			Env: map[string]string{
				"NATS_URL":                  natsjetstream.NetworkURL(),
				"NATS_CRAWLED_PAGE_SUBJECT": crawledPageSubject,
				"ELASTICSEARCH_URL":         elasticsearchNetworkURL(),
				"ELASTICSEARCH_INDEX":       elasticsearchIndex,
			},
		},
	})
	if err != nil {
		t.Fatalf("start textindexer container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "textindexer", container)
}

func textIndexerImage(t *testing.T) string {
	t.Helper()
	return requiredimage.FromEnv(t, envTextIndexerImage, "textindexer", "e2e")
}
