package yagonode

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type crawlerRuntimePolicyParityEntry struct {
	environment string
	setting     string
	bootstrap   string
}

func TestCrawlerRuntimePolicyBootstrapAndAdminDefaultsStayAligned(t *testing.T) {
	config, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load crawler bootstrap defaults: %v", err)
	}
	definitions := indexSettingDefinitions()
	nodeConfig := nodeConfig{Crawl: config}
	for _, entry := range crawlerRuntimePolicyParityEntries() {
		definition, found := definitions[entry.setting]
		if !found {
			t.Fatalf("setting %q is missing", entry.setting)
		}
		if definition.restartRequired() {
			t.Fatalf("setting %q does not propagate live", entry.setting)
		}
		value := definition.defaultValue(nodeConfig)
		if entry.environment == envCrawlerUserAgent {
			if value != config.RuntimePolicy.UserAgent {
				t.Fatalf(
					"setting %q default = %q, want %q",
					entry.setting,
					value,
					config.RuntimePolicy.UserAgent,
				)
			}
			continue
		}
		if value != entry.bootstrap {
			t.Fatalf("setting %q default = %q, want %q", entry.setting, value, entry.bootstrap)
		}
	}
}

func TestCrawlerRuntimePolicyDeploymentExamplesStayAligned(t *testing.T) {
	root := os.DirFS(filepath.Join("..", "..", ".."))
	rootEnvironment := readCrawlerPolicyDeploymentFile(t, root, ".env.example")
	compose := readCrawlerPolicyDeploymentFile(t, root, "docker-compose.yml.example")
	nodeEnvironment := readCrawlerPolicyDeploymentFile(
		t,
		root,
		filepath.Join("deploy", "systemd", "yago-node.env.example"),
	)
	crawlerEnvironment := readCrawlerPolicyDeploymentFile(
		t,
		root,
		filepath.Join("deploy", "systemd", "yago-crawler.env.example"),
	)
	for _, entry := range crawlerRuntimePolicyParityEntries() {
		plain := entry.environment + "=" + entry.bootstrap
		assertCrawlerPolicyDeploymentLine(t, ".env.example", rootEnvironment, plain, 1)
		assertCrawlerPolicyDeploymentLine(t, "yago-node.env.example", nodeEnvironment, plain, 1)
		assertCrawlerPolicyDeploymentLine(
			t,
			"yago-crawler.env.example",
			crawlerEnvironment,
			plain,
			1,
		)
		composeLine := entry.environment + ": ${" + entry.environment + ":-" + entry.bootstrap + "}"
		assertCrawlerPolicyDeploymentLine(t, "docker-compose.yml.example", compose, composeLine, 2)
	}
}

func crawlerRuntimePolicyParityEntries() []crawlerRuntimePolicyParityEntry {
	return []crawlerRuntimePolicyParityEntry{
		{envCrawlerAllowPrivateNetworks, settingKeyCrawlerAllowPrivateNetworks, "false"},
		{envCrawlerAllowCIDRs, settingKeyCrawlerAllowCIDRs, ""},
		{envCrawlerBrowserSandbox, settingKeyCrawlerBrowserSandbox, "false"},
		{envCrawlerBrowserFailureLimit, settingKeyCrawlerBrowserFailureLimit, "5"},
		{envCrawlerBrowserPath, settingKeyCrawlerBrowserPath, ""},
		{envCrawlerConnectTimeout, settingKeyCrawlerConnectTimeout, "5s"},
		{envCrawlerCrawlDelay, settingKeyCrawlerCrawlDelay, "1s"},
		{envCrawlerFrontierStateMaximumBytes, settingKeyCrawlerFrontierStateMaximumBytes, "4GB"},
		{envCrawlerHeaderTimeout, settingKeyCrawlerHeaderTimeout, "10s"},
		{envCrawlerMaximumDepth, settingKeyCrawlerMaximumDepth, "5"},
		{envCrawlerMaximumHostFetches, settingKeyCrawlerMaximumHostFetches, "2"},
		{envCrawlerMetricsAddress, settingKeyCrawlerMetricsAddress, ""},
		{envCrawlerRequestTimeout, settingKeyCrawlerRequestTimeout, "15s"},
		{envCrawlerRunPagesPerMinute, settingKeyCrawlerRunPagesPerMinute, "30"},
		{envCrawlerSitemapURLLimit, settingKeyCrawlerSitemapURLLimit, "10000"},
		{envCrawlerTLSTimeout, settingKeyCrawlerTLSTimeout, "5s"},
		{envCrawlerShutdownGrace, settingKeyCrawlerShutdownGrace, "10s"},
		{envCrawlerUserAgent, settingKeyCrawlerUserAgent, ""},
	}
}

func readCrawlerPolicyDeploymentFile(t *testing.T, source fs.FS, path string) []string {
	t.Helper()
	data, err := fs.ReadFile(source, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}

	return lines
}

func assertCrawlerPolicyDeploymentLine(
	t *testing.T,
	name string,
	lines []string,
	want string,
	count int,
) {
	t.Helper()
	found := 0
	for _, line := range lines {
		if line == want {
			found++
		}
	}
	if found != count {
		t.Fatalf("%s contains %q %d times, want %d", name, want, found, count)
	}
}
