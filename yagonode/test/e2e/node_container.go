//go:build e2e

package e2e

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	nodeContainerPort       = "8090"
	nodePublicContainerPort = "8080"
	nodePublicHTTPPort      = nodePublicContainerPort + "/tcp"
	envNodeImage            = "YAGO_NODE_IMAGE"
)

type nodeConfig struct {
	networkName string
	alias       string
	hash        yagomodel.Hash
	seedlistURL string
	extraEnv    map[string]string
}

func startNode(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	cfg nodeConfig,
) (testcontainers.Container, string) {
	t.Helper()
	env := map[string]string{
		"YAGO_PEER_BIRTH_DATE":               time.Now().AddDate(0, 0, -5).UTC().Format("20060102"),
		"YAGO_PEER_HASH":                     cfg.hash.String(),
		"YAGO_PEER_NAME":                     cfg.alias,
		"YAGO_NETWORK_NAME":                  yagoproto.DefaultNetwork,
		"YAGO_PEER_ADDR":                     ":" + nodeContainerPort,
		"YAGO_PUBLIC_ADDR":                   ":" + nodePublicContainerPort,
		"YAGO_ADVERTISE_HOST":                cfg.alias,
		"YAGO_ADVERTISE_PORT":                nodeContainerPort,
		"YAGO_DATA_DIR":                      "/tmp/data",
		"YAGO_ANNOUNCE_INTERVAL":             "10s",
		"YAGO_GREETS_PER_CYCLE":              strconv.Itoa(dhtMinConnectedPeers + 8),
		"YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS": "true",
		"LOG_LEVEL":                          "debug",
	}
	if cfg.seedlistURL != "" {
		env["YAGO_SEEDLIST_URLS"] = cfg.seedlistURL
	}
	for key, value := range cfg.extraEnv {
		env[key] = value
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          nodeImage(t),
			Name:           cfg.alias,
			ExposedPorts:   []string{httpPort, nodePublicHTTPPort},
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
