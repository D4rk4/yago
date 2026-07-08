//go:build e2e

// Package nodepeer starts and configures the node-under-test, alone or as a
// fleet.
package nodepeer

import (
	"context"
	"strconv"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerurl"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/egressproxy"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/requiredimage"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/peerclient"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	// MinConnectedPeers is the DHT sender gate's minimum connected-peer count.
	MinConnectedPeers = 33
	envNodeImage      = "YACY_NODE_IMAGE"
)

type Config struct {
	NetworkName string
	Alias       string
	Hash        yacymodel.Hash
	SeedlistURL string
}

func Start(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	cfg Config,
) (testcontainers.Container, string) {
	t.Helper()
	env := map[string]string{
		"YACY_PEER_HASH":         cfg.Hash.String(),
		"YACY_PEER_NAME":         cfg.Alias,
		"YACY_NETWORK_NAME":      yacyproto.DefaultNetwork,
		"YACY_PEER_ADDR":         ":" + peerclient.Port,
		"YACY_ADVERTISE_HOST":    cfg.Alias,
		"YACY_ADVERTISE_PORT":    peerclient.Port,
		"YACY_DATA_DIR":          "/tmp/data",
		"YACY_ANNOUNCE_INTERVAL": "10s",
		"YACY_GREETS_PER_CYCLE":  strconv.Itoa(MinConnectedPeers + 8),
		"YACY_PROXY_URL":         egressproxy.NetworkURL(),
		"LOG_LEVEL":              "debug",
	}
	if cfg.SeedlistURL != "" {
		env["YACY_SEEDLIST_URLS"] = cfg.SeedlistURL
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          image(t),
			Name:           cfg.Alias,
			ExposedPorts:   []string{peerclient.ExposedPort},
			Env:            env,
			Networks:       []string{cfg.NetworkName},
			NetworkAliases: map[string][]string{cfg.NetworkName: {cfg.Alias}},
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
	containerlog.DumpOnFailure(t, "node", container)
	nodeURL := containerurl.HostURL(t, ctx, container, peerclient.ExposedPort)
	if !pollwait.For(20*time.Second, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatalf("node %s never became reachable from the host", cfg.Alias)
	}
	return container, nodeURL
}

func image(t *testing.T) string {
	t.Helper()
	return requiredimage.FromEnv(t, envNodeImage, "node", "e2e")
}
