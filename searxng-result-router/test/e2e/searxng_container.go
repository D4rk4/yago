//go:build e2e

package e2e

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/containerlog"
)

const (
	searxngImage = "docker.io/searxng/searxng:2026.7.5-a6438586a" +
		"@sha256:5db870274800e0ed53ffe3c94806523f5313b00f5f7fc038f9e345e867c1f10b"
	searxngAlias      = "searxng"
	searxngPort       = "8080/tcp"
	pluginMountDir    = "/opt/e2e-plugins"
	pluginSourcePath  = "../../result_link_router.py"
	engineMountDir    = "/usr/local/searxng/searx/engines"
	testEngineModule  = "offline_test_engine"
	testEngineName    = "origin"
	testEngineBang    = "ot"
	originDestination = "http://example.invalid/origin-page"
)

func testEngineSource(originURL string) string {
	return `categories = ["general"]
about = {}


def request(query, params):
    params["url"] = "` + originURL + `"
    return params


def response(resp):
    return [
        {
            "title": "Origin page",
            "url": "` + originDestination + `",
            "content": "origin page content",
        }
    ]
`
}

const testSettingsYAML = `use_default_settings:
  engines:
    keep_only:
      - ` + testEngineName + `

server:
  secret_key: "e2e-test-secret-key"

search:
  formats:
    - html
    - json

engines:
  - name: ` + testEngineName + `
    engine: ` + testEngineModule + `
    shortcut: ` + testEngineBang + `
    categories: general
    disabled: false
    enable_http: true

plugins:
  result_link_router.SXNGPlugin:
    active: true
`

func startSearXNG(
	t *testing.T,
	ctx context.Context,
	networkName string,
	visitcrawlBaseURL string,
) string {
	t.Helper()
	pluginPath, err := filepath.Abs(pluginSourcePath)
	if err != nil {
		t.Fatalf("resolve plugin source path: %v", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          searxngImage,
			ExposedPorts:   []string{searxngPort},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {searxngAlias}},
			Env: map[string]string{
				"PYTHONPATH":              pluginMountDir,
				"YACYVISITCRAWL_BASE_URL": visitcrawlBaseURL,
			},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      pluginPath,
					ContainerFilePath: pluginMountDir + "/result_link_router.py",
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(testEngineSource(originNetworkURL())),
					ContainerFilePath: engineMountDir + "/" + testEngineModule + ".py",
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(testSettingsYAML),
					ContainerFilePath: "/etc/searxng/settings.yml",
					FileMode:          0o644,
				},
			},
			WaitingFor: wait.ForHTTP("/").
				WithPort(searxngPort).
				WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start searxng container %s: %v", searxngImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	containerlog.DumpOnFailure(t, "searxng", container)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve searxng host: %v", err)
	}
	port, err := container.MappedPort(ctx, searxngPort)
	if err != nil {
		t.Fatalf("resolve searxng mapped port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, port.Port())
}
