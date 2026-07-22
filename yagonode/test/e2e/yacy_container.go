//go:build e2e

package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const defaultYaCyImage = "docker.io/yacy/yacy_search_server@sha256:4225dd07b605347b62ff1fbfa0268217aa79ba2d29bdb0a76d5366d4267398da"

func startYaCy(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	networkName, alias string,
) (testcontainers.Container, string) {
	return startYaCyWithObserverNetwork(
		t,
		ctx,
		probe,
		networkName,
		"",
		alias,
	)
}

func startYaCyWithObserverNetwork(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	probeNetworkName, observerNetworkName, alias string,
) (testcontainers.Container, string) {
	t.Helper()
	image := os.Getenv("YAGO_YACY_IMAGE")
	if image == "" {
		image = defaultYaCyImage
	}
	const defaults = "/opt/yacy_search_server/defaults/"
	const unitFile = defaults + "yacy.network.freeworld.unit"
	staticIP := "sed -i 's#^staticIP=.*#staticIP=" + alias + "#' " + defaults + "yacy.init"
	if observerNetworkName != "" {
		staticIP = "advertised_host=$(getent ahostsv4 " + alias + " | awk 'NR == 1 {print $1}')" +
			" && test -n \"$advertised_host\"" +
			" && sed -i \"s#^staticIP=.*#staticIP=$advertised_host#\" " + defaults + "yacy.init"
	}
	setup := strings.Join([]string{
		"sed -i 's#<auth-method>DIGEST</auth-method>#<auth-method>BASIC</auth-method>#' " + defaults + "web.xml",
		"sed -i '/^network.unit.bootstrap.seedlist/d' " + unitFile,
		"sed -i 's#^network.unit.domain.*#network.unit.domain = any#' " + unitFile,
		staticIP,
		"sed -i 's#^allowReceiveIndex=.*#allowReceiveIndex=true#' " + defaults + "yacy.init",
		"sed -i 's#^allowDistributeIndex=.*#allowDistributeIndex=true#' " + defaults + "yacy.init",
		"sed -i 's#^allowDistributeIndexWhileCrawling=.*#allowDistributeIndexWhileCrawling=true#' " + defaults + "yacy.init",
		"sed -i 's#^allowDistributeIndexWhileIndexing=.*#allowDistributeIndexWhileIndexing=true#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtdistribution_loadprereq=.*#20_dhtdistribution_loadprereq=999.0#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtreceive_loadprereq=.*#20_dhtreceive_loadprereq=999.0#' " + defaults + "yacy.init",
		"sed -i 's#^30_peerping_loadprereq=.*#30_peerping_loadprereq=999.0#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtdistribution_idlesleep=.*#20_dhtdistribution_idlesleep=1000#' " + defaults + "yacy.init",
		"sed -i 's#^20_dhtdistribution_busysleep=.*#20_dhtdistribution_busysleep=0#' " + defaults + "yacy.init",
		"sed -i 's#^.level=.*#.level=FINE#' " + defaults + "yacy.logging",
	}, " && ")
	networks, networkAliases := containerNetworks(
		probeNetworkName,
		observerNetworkName,
		alias,
	)
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          image,
			ExposedPorts:   []string{httpPort},
			Networks:       networks,
			NetworkAliases: networkAliases,
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
	dumpLogsOnFailure(t, "yacy", container)
	yacyURL := hostURL(t, ctx, container)
	if !waitFor(60*time.Second, func() bool {
		return probe.OK(ctx, yacyURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("YaCy never became reachable from the host")
	}
	return container, yacyURL
}
