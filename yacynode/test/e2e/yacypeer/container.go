//go:build e2e

// Package yacypeer starts, restarts, and feeds documents into the real YaCy
// peer.
package yacypeer

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerurl"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/test/e2e/peerclient"
)

const defaultImage = "docker.io/yacy/yacy_search_server:latest"

func Start(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	networkName, alias string,
) (testcontainers.Container, string) {
	t.Helper()
	image := os.Getenv("YACY_YACY_IMAGE")
	if image == "" {
		image = defaultImage
	}
	const defaults = "/opt/yacy_search_server/defaults/"
	const unitFile = defaults + "yacy.network.freeworld.unit"
	setup := strings.Join([]string{
		"sed -i 's#<auth-method>DIGEST</auth-method>#<auth-method>BASIC</auth-method>#' " + defaults + "web.xml",
		"sed -i '/^network.unit.bootstrap.seedlist/d' " + unitFile,
		"sed -i 's#^network.unit.domain.*#network.unit.domain = any#' " + unitFile,
		"sed -i 's#^staticIP=.*#staticIP=" + alias + "#' " + defaults + "yacy.init",
		"sed -i 's#^allowDistributeIndex=.*#allowDistributeIndex=true#' " + defaults + "yacy.init",
		"sed -i 's#^allowDistributeIndexWhileCrawling=.*#allowDistributeIndexWhileCrawling=true#' " + defaults + "yacy.init",
		"sed -i 's#^allowDistributeIndexWhileIndexing=.*#allowDistributeIndexWhileIndexing=true#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtdistribution_loadprereq=.*#20_dhtdistribution_loadprereq=9.0#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtdistribution_idlesleep=.*#20_dhtdistribution_idlesleep=1000#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtdistribution_busysleep=.*#20_dhtdistribution_busysleep=0#' " + defaults + "yacy.init",
		"sed -i 's#^.level=.*#.level=FINE#' " + defaults + "yacy.logging",
	}, " && ")
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          image,
			ExposedPorts:   []string{peerclient.ExposedPort},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {alias}},
			WaitingFor:     wait.ForExec([]string{"true"}).WithStartupTimeout(2 * time.Minute),
			Cmd: []string{
				"/bin/sh", "-c",
				setup + " && exec /bin/sh /opt/yacy_search_server/startYACY.sh -f",
			},
		},
	})
	if err != nil {
		t.Fatalf("start YaCy container %s: %v", image, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "yacy", container)
	yacyURL := containerurl.HostURL(t, ctx, container, peerclient.ExposedPort)
	if !pollwait.For(60*time.Second, func() bool {
		return probe.OK(ctx, yacyURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("YaCy never became reachable from the host")
	}
	return container, yacyURL
}

func Restart(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	container testcontainers.Container,
) string {
	t.Helper()
	stopTimeout := 30 * time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop yacy: %v", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("restart yacy: %v", err)
	}
	yacyURL := containerurl.HostURL(t, ctx, container, peerclient.ExposedPort)
	if !pollwait.For(60*time.Second, func() bool {
		return probe.OK(ctx, yacyURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("YaCy never became reachable after restart")
	}
	return yacyURL
}
