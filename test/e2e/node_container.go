//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"

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

func startNode(t *testing.T, ctx context.Context, cfg nodeConfig) testcontainers.Container {
	t.Helper()
	env := map[string]string{
		"YACY_PEER_HASH":         cfg.hash.String(),
		"YACY_PEER_NAME":         cfg.alias,
		"YACY_NETWORK_NAME":      yacyproto.DefaultNetwork,
		"YACY_PEER_ADDR":         ":" + nodeContainerPort,
		"YACY_ADVERTISE_HOST":    cfg.alias,
		"YACY_ADVERTISE_PORT":    nodeContainerPort,
		"YACY_DATA_DIR":          "/tmp/data",
		"YACY_ANNOUNCE_INTERVAL": "2s",
		"LOG_LEVEL":              "debug",
	}
	if cfg.seedlistURL != "" {
		env["YACY_SEEDLIST_URLS"] = cfg.seedlistURL
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          nodeImage(t),
			Name:           cfg.alias,
			ExposedPorts:   []string{httpPort},
			Env:            env,
			Networks:       []string{cfg.networkName},
			NetworkAliases: map[string][]string{cfg.networkName: {cfg.alias}},
			Tmpfs:          map[string]string{"/tmp": "rw,mode=1777"},
			HostConfigModifier: func(hc *dockercontainer.HostConfig) {
				hc.ReadonlyRootfs = true
				hc.CapDrop = []string{"ALL"}
				hc.SecurityOpt = append(hc.SecurityOpt, "no-new-privileges")
			},
		},
	})
	if err != nil {
		t.Fatalf("start node container from Dockerfile: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "node", c)
	return c
}

func nodeImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv(envNodeImage)
	if image == "" {
		t.Fatalf("%s is not set; build the node image first (run via `make e2e`)", envNodeImage)
	}
	return image
}
