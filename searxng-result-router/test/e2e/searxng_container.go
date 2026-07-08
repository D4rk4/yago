//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/searxng"
)

const (
	searxngAlias      = "searxng"
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

	return searxng.Start(t, ctx, networkName, searxng.Config{
		Alias:        searxngAlias,
		SettingsYAML: testSettingsYAML,
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
		},
	})
}
