//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/testcontainers/testcontainers-go"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/searxng"
)

const (
	searxngAlias     = "searxng"
	engineMountDir   = "/usr/local/searxng/searx/engines"
	engineModule     = "crawled_text_search"
	engineName       = "crawled text search"
	engineBang       = "ct"
	engineSourcePath = "../../crawled_text_search.py"
)

func testSettingsYAML(elasticsearchURL string) string {
	return `use_default_settings:
  engines:
    keep_only:
      - ` + engineName + `

server:
  secret_key: "e2e-test-secret-key"

search:
  formats:
    - html
    - json

engines:
  - name: ` + engineName + `
    engine: ` + engineModule + `
    shortcut: ` + engineBang + `
    categories: general
    disabled: false
    enable_http: true
    elasticsearch_url: ` + elasticsearchURL + `
    elasticsearch_index: ` + elasticsearchIndex + `
`
}

func startSearXNG(t *testing.T, ctx context.Context, networkName, elasticsearchURL string) string {
	t.Helper()
	enginePath, err := filepath.Abs(engineSourcePath)
	if err != nil {
		t.Fatalf("resolve engine source path: %v", err)
	}

	return searxng.Start(t, ctx, networkName, searxng.Config{
		Alias:        searxngAlias,
		SettingsYAML: testSettingsYAML(elasticsearchURL),
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      enginePath,
				ContainerFilePath: engineMountDir + "/" + engineModule + ".py",
				FileMode:          0o644,
			},
		},
	})
}
