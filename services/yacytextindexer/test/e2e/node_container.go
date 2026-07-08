//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/egressproxy"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/natsjetstream"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/requiredimage"
)

const (
	nodeAlias    = "node"
	nodePeerHash = "E2ETEXTINDX1"
	envNodeImage = "YACY_NODE_IMAGE"
)

func startNode(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          nodeImage(t),
			Name:           nodeAlias,
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {nodeAlias}},
			Env: map[string]string{
				"YACY_PEER_HASH":      nodePeerHash,
				"YACY_PEER_NAME":      nodeAlias,
				"YACY_ADVERTISE_HOST": nodeAlias,
				"NATS_URL":            natsjetstream.NetworkURL(),
				"YACY_PROXY_URL":      egressproxy.NetworkURL(),
				"LOG_LEVEL":           "debug",
			},
		},
	})
	if err != nil {
		t.Fatalf("start node container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "node", container)
}

func nodeImage(t *testing.T) string {
	t.Helper()
	return requiredimage.FromEnv(t, envNodeImage, "node", "e2e")
}
