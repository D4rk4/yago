//go:build e2e

package e2e

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	envNodeImage    = "YAGO_NODE_IMAGE"
	nodeAlias       = "node"
	nodePeerPort    = "8090"
	nodePublicPort  = "8080"
	nodeOpsPort     = "9090"
	nodeCrawlRPCEnv = ":9091"

	// nodePeerHash is a fixed valid peer hash minted by cmd/yago-peer-hash; the
	// node runs standalone here so any well-formed hash is sufficient.
	nodePeerHash  = "ziv-MzQvviRK"
	nodeAdminUser = "admin"
	nodeAdminPass = "crawl-e2e-pass"
)

type nodeBroker struct {
	opsURL    string
	publicURL string
}

func startNodeBroker(t *testing.T, ctx context.Context, networkName string) nodeBroker {
	t.Helper()
	image := requireImage(t, envNodeImage, "node")
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image: image,
			Name:  nodeAlias,
			ExposedPorts: []string{
				nodePeerPort + "/tcp",
				nodePublicPort + "/tcp",
				nodeOpsPort + "/tcp",
			},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {nodeAlias}},
			Env: map[string]string{
				"YAGO_PEER_HASH":      nodePeerHash,
				"YAGO_PEER_NAME":      "lease-e2e-node",
				"YAGO_PEER_ADDR":      ":" + nodePeerPort,
				"YAGO_PUBLIC_ADDR":    ":" + nodePublicPort,
				"YAGO_OPS_ADDR":       ":" + nodeOpsPort,
				"YAGO_ADVERTISE_HOST": nodeAlias,
				"YAGO_ADVERTISE_PORT": nodePeerPort,
				"YAGO_NETWORK_NAME":   "freeworld",
				"YAGO_PEER_BIRTH_DATE": time.Now().
					AddDate(0, 0, -1).
					UTC().
					Format("20060102"),
				"YAGO_DATA_DIR":                      "/tmp/data",
				"YAGO_CRAWL_RPC_ADDR":                nodeCrawlRPCEnv,
				"YAGO_ADMIN_USER":                    nodeAdminUser,
				"YAGO_ADMIN_PASSWORD":                nodeAdminPass,
				"YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS": "true",
				"LOG_LEVEL":                          "info",
			},
			Tmpfs: map[string]string{"/tmp": "rw,mode=1777"},
			HostConfigModifier: func(hostConfig *dockercontainer.HostConfig) {
				hostConfig.ReadonlyRootfs = true
				hostConfig.CapDrop = []string{"ALL"}
				hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "no-new-privileges")
			},
			WaitingFor: wait.ForHTTP("/health").
				WithPort(nodeOpsPort + "/tcp").
				WithStartupTimeout(90 * time.Second),
		},
	})
	if err != nil {
		t.Fatalf("start node container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "node", container)

	return nodeBroker{
		opsURL:    mappedBaseURL(t, ctx, container, nodeOpsPort),
		publicURL: mappedBaseURL(t, ctx, container, nodePublicPort),
	}
}

func mappedBaseURL(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	port string,
) string {
	t.Helper()
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	mapped, err := container.MappedPort(ctx, port+"/tcp")
	if err != nil {
		t.Fatalf("mapped port %s: %v", port, err)
	}

	return "http://" + net.JoinHostPort(host, mapped.Port())
}

func requireImage(t *testing.T, env, label string) string {
	t.Helper()
	image := os.Getenv(env)
	if image == "" {
		t.Skipf("%s is not set; build the %s image first (run via `make e2e`)", env, label)
	}

	return image
}
