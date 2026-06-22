//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	nodeContainerPort = "8090"
	envNodeImage      = "YACY_NODE_IMAGE"
)

type nodeConfig struct {
	networkName string
	alias       string
	hash        yacymodel.Hash
	seedlistURL string
}

func startNode(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	cfg nodeConfig,
) (testcontainers.Container, string) {
	t.Helper()
	env := map[string]string{
		"YACY_PEER_HASH":         cfg.hash.String(),
		"YACY_PEER_NAME":         cfg.alias,
		"YACY_NETWORK_NAME":      yacyproto.DefaultNetwork,
		"YACY_PEER_ADDR":         ":" + nodeContainerPort,
		"YACY_ADVERTISE_HOST":    cfg.alias,
		"YACY_ADVERTISE_PORT":    nodeContainerPort,
		"YACY_DATA_DIR":          "/tmp/data",
		"YACY_ANNOUNCE_INTERVAL": "10s",
		"LOG_LEVEL":              "debug",
	}
	if cfg.seedlistURL != "" {
		env["YACY_SEEDLIST_URLS"] = cfg.seedlistURL
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          nodeImage(t),
			Name:           cfg.alias,
			ExposedPorts:   []string{httpPort},
			Env:            env,
			Networks:       []string{cfg.networkName},
			NetworkAliases: map[string][]string{cfg.networkName: {cfg.alias}},
			Tmpfs:          map[string]string{"/tmp": "rw,mode=1777"},
			HostConfigModifier: func(hostConfig *dockercontainer.HostConfig) {
				hostConfig.ReadonlyRootfs = true
				hostConfig.CapDrop = []string{"ALL"}
				hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "no-new-privileges")
			},
		},
	})
	if err != nil {
		t.Fatalf("start node container from Dockerfile: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "node", container)
	nodeURL := hostURL(t, ctx, container)
	if !waitFor(20*time.Second, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatalf("node %s never became reachable from the host", cfg.alias)
	}
	return container, nodeURL
}

func nodeImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv(envNodeImage)
	if image == "" {
		t.Fatalf("%s is not set; build the node image first (run via `make e2e`)", envNodeImage)
	}
	return image
}
