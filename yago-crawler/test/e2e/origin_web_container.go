//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	originImage = "docker.io/library/nginx:alpine"
	originAlias = "origin"
	originPage  = `<!DOCTYPE html>
<html lang="en">
  <head><title>Reliable distributed search</title></head>
  <body><p>Modern search nodes collect useful words from public documents and preserve their context for accurate retrieval. The crawler respects robots policies, host pacing, redirect limits, and document size bounds while it discovers pages. Extracted titles, headings, anchor text, and body passages enter a local lexical index. Query processing combines strict matching with bounded recall expansion, field evidence, proximity, freshness, quality, and domain authority. Federated peers contribute compatible results without replacing the local document store. Operators can inspect every ranking signal, train a model from reviewed judgments, compare held out metrics, and keep the existing model whenever evidence is insufficient. This page provides a deterministic end to end fixture for that complete flow.</p></body>
</html>`
)

func startOrigin(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          originImage,
			ExposedPorts:   []string{"80/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {originAlias}},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(originPage),
				ContainerFilePath: "/usr/share/nginx/html/index.html",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForHTTP("/").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start origin container %s: %v", originImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "origin", container)
	return "http://" + originAlias + "/"
}
