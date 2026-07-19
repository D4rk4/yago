package yagonode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
)

type environmentControlClass string

const (
	environmentSettingBacked     environmentControlClass = "setting-backed"
	environmentDedicatedAdmin    environmentControlClass = "dedicated-admin-or-sensitive"
	environmentBootstrapIdentity environmentControlClass = "bootstrap-topology-or-identity"
	environmentHostFacility      environmentControlClass = "deployment-host-facility"
	environmentMigrationOnly     environmentControlClass = "migration-only"
)

type environmentConsumer uint8

const (
	environmentNodeConsumer environmentConsumer = 1 << iota
	environmentCrawlerConsumer
)

type environmentControlCatalogEntry struct {
	class                  environmentControlClass
	setting                string
	consumers              environmentConsumer
	derivedBootstrapValue  bool
	targetSpecificFacility bool
	dedicatedAdminSurface  string
	bootstrapCredential    bool
}

type environmentCatalogAdder func(string, environmentControlCatalogEntry)

type environmentDefaultAssertion struct {
	definitions map[string]settingDefinition
	settings    map[string]string
	bootstrap   map[string]string
	config      nodeConfig
}

type composeSourceLine struct {
	number int
	text   string
}

type consumerDeploymentEnvironment struct {
	compose  map[string]string
	systemd  map[string]string
	consumer environmentConsumer
	name     string
}

func TestRuntimeEnvironmentControlsAreClassified(t *testing.T) {
	catalog := runtimeEnvironmentControlCatalog(t)
	discovered := discoverRuntimeEnvironmentControls(t)
	for name := range discovered {
		if _, found := catalog[name]; !found {
			t.Errorf("runtime environment control %s is not classified", name)
		}
	}
	validClasses := map[environmentControlClass]struct{}{
		environmentSettingBacked: {}, environmentDedicatedAdmin: {},
		environmentBootstrapIdentity: {}, environmentHostFacility: {},
		environmentMigrationOnly: {},
	}
	for name, entry := range catalog {
		if _, found := discovered[name]; !found {
			t.Errorf("classified environment control %s is not read by either runtime", name)
		}
		if _, valid := validClasses[entry.class]; !valid {
			t.Errorf("environment control %s has invalid class %q", name, entry.class)
		}
		if entry.consumers == 0 {
			t.Errorf("environment control %s has no runtime consumer", name)
		}
	}
}

func TestCanonicalEnvironmentOnlyExceptionsRemainExact(t *testing.T) {
	catalog := runtimeEnvironmentControlCatalog(t)
	mismatches := canonicalEnvironmentOnlyExceptionMismatches(catalog)
	for name := range mismatches {
		t.Errorf("environment-only exception %s is outside the canonical allowlist", name)
	}
	withFutureEscape := make(map[string]environmentControlCatalogEntry, len(catalog)+1)
	for name, entry := range catalog {
		withFutureEscape[name] = entry
	}
	withFutureEscape["YAGO_FUTURE_HOST_ESCAPE"] = environmentControlCatalogEntry{
		class:     environmentHostFacility,
		consumers: environmentNodeConsumer,
	}
	if _, found := canonicalEnvironmentOnlyExceptionMismatches(
		withFutureEscape,
	)["YAGO_FUTURE_HOST_ESCAPE"]; !found {
		t.Fatal("future generic host-facility escape was not detected")
	}
}

func canonicalEnvironmentOnlyExceptionMismatches(
	catalog map[string]environmentControlCatalogEntry,
) map[string]struct{} {
	expected := map[string]environmentControlClass{
		envDataDir:                   environmentHostFacility,
		envPeerHash:                  environmentBootstrapIdentity,
		envPeerBirthDate:             environmentBootstrapIdentity,
		"YAGO_CRAWLER_WORKER_ID":     environmentBootstrapIdentity,
		"YAGO_CRAWLER_NODE_RPC_ADDR": environmentBootstrapIdentity,
	}
	mismatches := make(map[string]struct{})
	for name, class := range expected {
		entry, found := catalog[name]
		if !found || entry.class != class {
			mismatches[name] = struct{}{}
		}
	}
	for name, entry := range catalog {
		if entry.class != environmentBootstrapIdentity && entry.class != environmentHostFacility {
			continue
		}
		if class, found := expected[name]; !found || class != entry.class {
			mismatches[name] = struct{}{}
		}
	}

	return mismatches
}

func TestRuntimeEnvironmentDiscoveryIncludesLiteralLookups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment_fixture.go")
	source := []byte(`package fixture
import runtimeos "os"
const namedEnvironment = "YAGO_NAMED_CONTROL"
var variableEnvironment = "YAGO_VARIABLE_CONTROL"
var composedEnvironment = "YAGO_" + "COMPOSED_CONTROL"
func readEnvironment(readRuntimeValue func(string) string) {
	var localEnvironment = "YAGO_LOCAL_VARIABLE_CONTROL"
	_ = runtimeos.Getenv(variableEnvironment)
	_, _ = runtimeos.LookupEnv("YAGO_ALIASED_LITERAL_CONTROL")
	_ = readRuntimeValue(namedEnvironment)
	_ = readRuntimeValue(composedEnvironment)
	_ = readRuntimeValue(localEnvironment)
	_ = readRuntimeValue("YAGO_CALLBACK_LITERAL_CONTROL")
}
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write environment fixture: %v", err)
	}
	discovered := environmentControlsInFile(t, path)
	for _, name := range []string{
		"YAGO_NAMED_CONTROL",
		"YAGO_VARIABLE_CONTROL",
		"YAGO_COMPOSED_CONTROL",
		"YAGO_LOCAL_VARIABLE_CONTROL",
		"YAGO_ALIASED_LITERAL_CONTROL",
		"YAGO_CALLBACK_LITERAL_CONTROL",
	} {
		if _, found := discovered[name]; !found {
			t.Errorf("environment control %s was not discovered", name)
		}
	}
}

func TestSettingBackedEnvironmentControlsMatchAdminDefaults(t *testing.T) {
	root := os.DirFS(filepath.Join("..", "..", ".."))
	bootstrap := readEnvironmentAssignments(t, root, ".env.example")
	getenv := func(name string) string { return bootstrap[name] }
	config, err := loadNodeConfig(getenv)
	if err != nil {
		t.Fatalf("load deployment bootstrap defaults: %v", err)
	}
	config.Crawl, err = loadRuntimeCrawlConfig(getenv, config.DataDir)
	if err != nil {
		t.Fatalf("load crawler deployment bootstrap defaults: %v", err)
	}
	config.CrossOrigin, err = loadCrossOriginConfig(getenv)
	if err != nil {
		t.Fatalf("load cross-origin deployment bootstrap defaults: %v", err)
	}
	assertion := environmentDefaultAssertion{
		definitions: indexSettingDefinitions(),
		settings:    make(map[string]string),
		bootstrap:   bootstrap,
		config:      config,
	}
	for environment, entry := range runtimeEnvironmentControlCatalog(t) {
		assertion.assertSettingBackedEnvironmentDefault(t, environment, entry)
	}
}

func (assertion *environmentDefaultAssertion) assertSettingBackedEnvironmentDefault(
	t *testing.T,
	environment string,
	entry environmentControlCatalogEntry,
) {
	t.Helper()
	if entry.class == environmentSettingBacked && entry.setting == "" {
		t.Errorf("setting-backed control %s has no Admin setting", environment)
		return
	}
	if entry.setting == "" {
		if entry.class == environmentDedicatedAdmin {
			assertDedicatedEnvironmentBoundary(t, environment, entry)
			if entry.dedicatedAdminSurface != "" {
				assertion.assertDedicatedBindingDefault(t, environment, entry)
			}
		}
		return
	}
	definition, found := assertion.definitions[entry.setting]
	if !found {
		t.Errorf("environment control %s maps to missing setting %s", environment, entry.setting)
		return
	}
	if previous, duplicate := assertion.settings[entry.setting]; duplicate {
		t.Errorf("setting %s maps to both %s and %s", entry.setting, previous, environment)
	}
	assertion.settings[entry.setting] = environment
	if entry.class == environmentDedicatedAdmin {
		assertSensitiveEnvironmentSetting(t, environment, entry.setting, definition)
		return
	}
	assertion.assertEnvironmentSettingDefault(t, environment, entry, definition)
}

func (assertion environmentDefaultAssertion) assertDedicatedBindingDefault(
	t *testing.T,
	environment string,
	entry environmentControlCatalogEntry,
) {
	t.Helper()

	raw, found := assertion.bootstrap[environment]
	if !found {
		t.Errorf("bootstrap default for %s is missing", environment)
		return
	}
	host, port, err := splitBindAddr(raw)
	if err != nil {
		t.Errorf("bootstrap default for %s is invalid: %v", environment, err)
		return
	}
	definition, found := indexBindDefinitions()[entry.dedicatedAdminSurface]
	if !found {
		t.Errorf("Admin bind surface %s is missing", entry.dedicatedAdminSurface)
		return
	}
	actual := definition.current(assertion.config)
	expected := formatBindAddr(host, port)
	if actual != expected {
		t.Errorf("%s Admin default = %q, bootstrap default = %q", environment, actual, expected)
	}
}

func assertDedicatedEnvironmentBoundary(
	t *testing.T,
	environment string,
	entry environmentControlCatalogEntry,
) {
	t.Helper()
	if entry.dedicatedAdminSurface != "" {
		expected := map[string]string{
			envCrawlRPCAddr: bindKeyCrawler,
			envPeerAddr:     bindKeyPeer,
			envPublicAddr:   bindKeyPublic,
			envOpsAddr:      bindKeyOps,
		}[environment]
		if expected == "" || entry.dedicatedAdminSurface != expected {
			t.Errorf(
				"dedicated control %s maps to Admin bind surface %s, want %s",
				environment,
				entry.dedicatedAdminSurface,
				expected,
			)
			return
		}
		definition, found := indexBindDefinitions()[entry.dedicatedAdminSurface]
		if !found || definition.key != entry.dedicatedAdminSurface {
			t.Errorf(
				"dedicated control %s maps to missing Admin bind surface %s",
				environment,
				entry.dedicatedAdminSurface,
			)
		}
		return
	}
	if entry.bootstrapCredential && bootstrapCredentialHasAdminAlternative(environment) {
		return
	}
	t.Errorf("dedicated control %s has no proven Admin or credential boundary", environment)
}

func bootstrapCredentialHasAdminAlternative(environment string) bool {
	switch environment {
	case envAdminUser, envAdminPassword:
		return adminauth.PathSetupPage != "" && adminauth.PathLoginPage != ""
	case envSearchAccessToken:
		for _, group := range securityScopeGroups() {
			for _, option := range group.Scopes {
				if option.Value == string(adminauth.ScopeSearchRead) {
					return true
				}
			}
		}
	}

	return false
}

func assertSensitiveEnvironmentSetting(
	t *testing.T,
	environment string,
	setting string,
	definition settingDefinition,
) {
	t.Helper()
	if !definition.sensitive {
		t.Errorf("dedicated sensitive control %s maps to readable setting %s", environment, setting)
	}
}

func (assertion environmentDefaultAssertion) assertEnvironmentSettingDefault(
	t *testing.T,
	environment string,
	entry environmentControlCatalogEntry,
	definition settingDefinition,
) {
	t.Helper()
	if definition.sensitive {
		t.Errorf(
			"setting-backed control %s unexpectedly maps to sensitive setting %s",
			environment,
			entry.setting,
		)
		return
	}
	raw, found := assertion.bootstrap[environment]
	if !found {
		t.Errorf("bootstrap default for %s is missing", environment)
		return
	}
	if entry.derivedBootstrapValue {
		assertDerivedEnvironmentDefault(
			t,
			environment,
			raw,
			definition.defaultValue(assertion.config),
		)
		return
	}
	normalized, err := definition.normalize(raw)
	if err != nil {
		t.Errorf("normalize %s bootstrap value %q: %v", environment, raw, err)
		return
	}
	if actual := definition.defaultValue(assertion.config); actual != normalized {
		t.Errorf("%s Admin default = %q, bootstrap default = %q", environment, actual, normalized)
	}
}

func assertDerivedEnvironmentDefault(t *testing.T, environment, raw, effective string) {
	t.Helper()
	if raw != "" {
		t.Errorf("derived bootstrap control %s = %q, want empty", environment, raw)
	}
	if effective == "" {
		t.Errorf("derived Admin default for %s is empty", environment)
	}
}

func TestDeploymentExamplesCoverRuntimeEnvironmentControls(t *testing.T) {
	root := os.DirFS(filepath.Join("..", "..", ".."))
	rootEnvironment := readEnvironmentAssignments(t, root, ".env.example")
	nodeSystemd := readEnvironmentAssignments(
		t,
		root,
		filepath.Join("deploy", "systemd", "yago-node.env.example"),
	)
	crawlerSystemd := readEnvironmentAssignments(
		t,
		root,
		filepath.Join("deploy", "systemd", "yago-crawler.env.example"),
	)
	nodeCompose := readComposeEnvironment(t, root, "yago-node")
	crawlerCompose := readComposeEnvironment(t, root, "yago-crawler")
	catalog := runtimeEnvironmentControlCatalog(t)

	assertKnownDeploymentEnvironment(
		t,
		".env.example",
		rootEnvironment,
		catalog,
		map[string]struct{}{
			"SOURCE_REVISION": {},
			"VERSION":         {},
		},
	)
	assertKnownDeploymentEnvironment(t, "yago-node Compose service", nodeCompose, catalog, nil)
	assertKnownDeploymentEnvironment(
		t,
		"yago-crawler Compose service",
		crawlerCompose,
		catalog,
		nil,
	)
	assertKnownDeploymentEnvironment(t, "yago-node systemd environment", nodeSystemd, catalog, nil)
	assertKnownDeploymentEnvironment(
		t,
		"yago-crawler systemd environment",
		crawlerSystemd,
		catalog,
		nil,
	)

	for environment, entry := range catalog {
		if entry.class == environmentMigrationOnly {
			assertEnvironmentAbsent(
				t,
				environment,
				rootEnvironment,
				nodeCompose,
				crawlerCompose,
				nodeSystemd,
				crawlerSystemd,
			)
			continue
		}
		rootValue, found := rootEnvironment[environment]
		if !found {
			t.Errorf(".env.example omits %s", environment)
			continue
		}
		nodeDeployment := consumerDeploymentEnvironment{
			compose:  nodeCompose,
			systemd:  nodeSystemd,
			consumer: environmentNodeConsumer,
			name:     "node",
		}
		nodeDeployment.assertEnvironmentCoverage(t, environment, rootValue, entry)
		crawlerDeployment := consumerDeploymentEnvironment{
			compose: crawlerCompose, systemd: crawlerSystemd,
			consumer: environmentCrawlerConsumer, name: "crawler",
		}
		crawlerDeployment.assertEnvironmentCoverage(t, environment, rootValue, entry)
	}
}

func runtimeEnvironmentControlCatalog(t *testing.T) map[string]environmentControlCatalogEntry {
	t.Helper()
	entries := map[string]environmentControlCatalogEntry{}
	add := environmentCatalogAdder(func(name string, entry environmentControlCatalogEntry) {
		if _, duplicate := entries[name]; duplicate {
			t.Fatalf("duplicate environment catalog entry %s", name)
		}
		entries[name] = entry
	})
	addNodeStorageAndNetworkEnvironmentSettings(add)
	addNodeSearchEnvironmentSettings(add)
	addNodePeerEnvironmentSettings(add)
	addCrawlerCapacityEnvironmentSettings(add)
	addCrawlerPolicyEnvironmentSettings(add)
	addDHTAndExtractEnvironmentSettings(add)
	addRemoteCrawlEnvironmentSettings(add)
	addWebFallbackEnvironmentSettings(add)
	addRuntimeEnvironmentFacilities(add)
	addMigrationEnvironmentControls(t, entries, add)

	return entries
}

func addEnvironmentSetting(
	add environmentCatalogAdder,
	name string,
	key string,
	consumers environmentConsumer,
) {
	add(name, environmentControlCatalogEntry{
		class: environmentSettingBacked, setting: key, consumers: consumers,
	})
}

func addNodeStorageAndNetworkEnvironmentSettings(add environmentCatalogAdder) {
	addEnvironmentSetting(add, envPeerName, "peer.name", environmentNodeConsumer)
	addEnvironmentSetting(add, envAdvertiseHost, "network.advertise.host", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envAdvertisePort,
		settingKeyNetworkAdvertisePort,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envPublicSelfTestURL,
		settingKeyNetworkPublicSelfTest,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envStorageQuota, "storage.quota", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envStorageReservedFree,
		settingKeyStorageReservedFree,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envStorageHysteresis,
		settingKeyStoragePressureHysteresis,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envStorageCompaction,
		"storage.compaction.interval",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envStorageAutosplit, "storage.autosplit", environmentNodeConsumer)
	addEnvironmentSetting(add, envStorageDeferFsync, "storage.defer_fsync", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envStorageReadDefer,
		settingKeyStorageReadDefer,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envTrustedProxies,
		"security.trusted_proxies",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envEgressAllowLAN,
		"security.egress.allow_private",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envEgressAllowCIDRs,
		"security.egress.allow_cidrs",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envSeedlistURLs, "network.seedlists", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envAnnounceInterval,
		"network.announce.interval",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envGreetsPerCycle,
		"network.announce.greets_per_cycle",
		environmentNodeConsumer,
	)
}

func addNodeSearchEnvironmentSettings(add environmentCatalogAdder) {
	addEnvironmentSetting(add, envLogLevel, settingKeyLoggingLevel, environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envSearchRequireAPIKey,
		"search.api.scoped_access",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envPublicSearchUI,
		settingKeyPublicSearchPortal,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envHTTPSRedirect, settingKeyHTTPSRedirect, environmentNodeConsumer)
	addEnvironmentSetting(add, envPublicBaseURL, settingKeyPublicBaseURL, environmentNodeConsumer)
	addEnvironmentSetting(add, envQueryLogMode, "search.query.log", environmentNodeConsumer)
	addEnvironmentSetting(add, envMetricsEnabled, "metrics.enabled", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envAdminRestartEnabled,
		settingKeyAdminRestartControls,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envIndexRemoteResults,
		"search.index.remote",
		environmentNodeConsumer,
	)
}

func addNodePeerEnvironmentSettings(add environmentCatalogAdder) {
	addEnvironmentSetting(add, envNetworkName, settingKeyNetworkName, environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envPeerHTTPSPreferred,
		"network.peer.https_preferred",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envAdvertiseDirect,
		"peer.advertise.direct_connect",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envAdvertiseRemoteIndex,
		"peer.advertise.remote_index",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envAdvertiseRootNode,
		"peer.advertise.root_node",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envAdvertiseSSL, "peer.advertise.ssl", environmentNodeConsumer)
	addEnvironmentSetting(add, envSearchLinksNewTab, "search.links.newtab", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envSearchClickCapture,
		"search.click.capture",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envSwarmSeedCrawl, "swarm.seed.enabled", environmentNodeConsumer)
	addEnvironmentSetting(add, envSwarmSeedDepth, "swarm.seed.depth", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envSwarmSeedMaxPages,
		"swarm.seed.max_pages",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envSwarmMorphology,
		"swarm.morphology.enabled",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envIngestQualityGate,
		"crawl.ingest.quality_gate",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envPeerSnippetFetch,
		"search.peer.snippet_fetch",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemotePeerTimeout,
		"search.remote.peer_timeout",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envLANDiscovery, "network.lan_discovery", environmentNodeConsumer)
	addEnvironmentSetting(add, envRemoteTimeout, "search.remote.timeout", environmentNodeConsumer)
	addEnvironmentSetting(add, envAdminCORSOrigins, "security.cors.admin", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envSearchCORSOrigins,
		"security.cors.search",
		environmentNodeConsumer,
	)
}

func addCrawlerCapacityEnvironmentSettings(add environmentCatalogAdder) {
	consumers := environmentNodeConsumer | environmentCrawlerConsumer
	addEnvironmentSetting(add, envCrawlerWorkers, settingKeyCrawlerFetchWorkers, consumers)
	addEnvironmentSetting(
		add,
		envCrawlerProcessPagesPerSecond,
		settingKeyCrawlerProcessPagesPerSecond,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerMaximumRedirects,
		settingKeyCrawlerMaximumRedirects,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerMaxActiveRuns,
		settingKeyCrawlerMaximumActiveRuns,
		consumers,
	)
	addEnvironmentSetting(add, envCrawlerMaxPagesPerRun, settingKeyCrawlerMaxPagesPerRun, consumers)
	addEnvironmentSetting(
		add,
		envPrioritizeAutomaticDiscovery,
		settingKeyPrioritizeAutomaticDiscovery,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerStorageReservedFree,
		settingKeyCrawlerStorageReservedFree,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerStorageHysteresis,
		settingKeyCrawlerStorageHysteresis,
		consumers,
	)
}

func addCrawlerPolicyEnvironmentSettings(add environmentCatalogAdder) {
	consumers := environmentNodeConsumer | environmentCrawlerConsumer
	addEnvironmentSetting(
		add,
		envCrawlerAllowPrivateNetworks,
		settingKeyCrawlerAllowPrivateNetworks,
		consumers,
	)
	addEnvironmentSetting(add, envCrawlerAllowCIDRs, settingKeyCrawlerAllowCIDRs, consumers)
	addEnvironmentSetting(
		add,
		envCrawlerBrowserSandbox,
		settingKeyCrawlerBrowserSandbox,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerBrowserFailureLimit,
		settingKeyCrawlerBrowserFailureLimit,
		consumers,
	)
	addEnvironmentSetting(add, envCrawlerBrowserPath, settingKeyCrawlerBrowserPath, consumers)
	addEnvironmentSetting(add, envCrawlerConnectTimeout, settingKeyCrawlerConnectTimeout, consumers)
	addEnvironmentSetting(add, envCrawlerCrawlDelay, settingKeyCrawlerCrawlDelay, consumers)
	addEnvironmentSetting(add, envCrawlerHeaderTimeout, settingKeyCrawlerHeaderTimeout, consumers)
	addEnvironmentSetting(add, envCrawlerMaximumDepth, settingKeyCrawlerMaximumDepth, consumers)
	addEnvironmentSetting(
		add,
		envCrawlerMaximumHostFetches,
		settingKeyCrawlerMaximumHostFetches,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerMetricsAddress,
		settingKeyCrawlerMetricsAddress,
		consumers,
	)
	addEnvironmentSetting(add, envCrawlerRequestTimeout, settingKeyCrawlerRequestTimeout, consumers)
	addEnvironmentSetting(
		add,
		envCrawlerRunPagesPerMinute,
		settingKeyCrawlerRunPagesPerMinute,
		consumers,
	)
	addEnvironmentSetting(
		add,
		envCrawlerSitemapURLLimit,
		settingKeyCrawlerSitemapURLLimit,
		consumers,
	)
	addEnvironmentSetting(add, envCrawlerTLSTimeout, settingKeyCrawlerTLSTimeout, consumers)
	addEnvironmentSetting(add, envCrawlerShutdownGrace, settingKeyCrawlerShutdownGrace, consumers)
	add(envCrawlerUserAgent, environmentControlCatalogEntry{
		class: environmentSettingBacked, setting: settingKeyCrawlerUserAgent,
		consumers: consumers, derivedBootstrapValue: true,
	})
}

func addDHTAndExtractEnvironmentSettings(add environmentCatalogAdder) {
	addEnvironmentSetting(add, envNetworkDHT, "dht.enabled", environmentNodeConsumer)
	addEnvironmentSetting(add, envDHTDistribution, "dht.distribution", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envDHTAllowWhileCrawling,
		"dht.allow_while_crawling",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envDHTAllowWhileIndexing,
		"dht.allow_while_indexing",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envDHTDistributionInterval, "dht.interval", environmentNodeConsumer)
	addEnvironmentSetting(add, envDHTRedundancy, "dht.redundancy", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envDHTPartitionExponent,
		settingKeyDHTPartitionExponent,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envDHTMinimumPeerAgeDays,
		"dht.min_peer_age_days",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envDHTMinimumConnectedPeers,
		"dht.min_connected_peers",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(add, envDHTMinimumRWIWords, "dht.min_rwi_words", environmentNodeConsumer)
	addEnvironmentSetting(
		add,
		envExtractFetchEnabled,
		"extract.fetch.enabled",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envExtractFetchTimeout,
		"extract.fetch.timeout",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envExtractFetchMaxBytes,
		"extract.fetch.max_bytes",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envNetworkAuthentication,
		settingKeyNetworkAuthenticationMode,
		environmentNodeConsumer,
	)
}

func addRemoteCrawlEnvironmentSettings(add environmentCatalogAdder) {
	add(envNetworkAuthenticationMaterial, environmentControlCatalogEntry{
		class: environmentDedicatedAdmin, setting: settingKeyNetworkAuthenticationSecret,
		consumers: environmentNodeConsumer,
	})
	addEnvironmentSetting(
		add,
		envRemoteCrawlEnabled,
		settingKeyRemoteCrawlEnabled,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemoteCrawlTrustedPeers,
		settingKeyRemoteCrawlTrustedPeers,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemoteCrawlAllowedDestinations,
		settingKeyRemoteCrawlAllowedDestinations,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemoteCrawlRequestsPerMinute,
		settingKeyRemoteCrawlRequestsPerMinute,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemoteCrawlOutstandingPerPeer,
		settingKeyRemoteCrawlOutstandingPerPeer,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemoteCrawlLeaseTTL,
		settingKeyRemoteCrawlLeaseTTL,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envRemoteCrawlQueueCapacity,
		settingKeyRemoteCrawlQueueCapacity,
		environmentNodeConsumer,
	)
}

func addWebFallbackEnvironmentSettings(add environmentCatalogAdder) {
	addEnvironmentSetting(
		add,
		envWebFallbackPrivacy,
		settingKeyWebFallbackPrivacy,
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackBackend,
		"web.fallback.backend",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackMaxResults,
		"web.fallback.max_results",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackTimeout,
		"web.fallback.timeout",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackSafeSearch,
		"web.fallback.safesearch",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackCacheTTL,
		"web.fallback.cache_ttl",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackSeedCrawl,
		"web.fallback.seed_crawl",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackSeedDepth,
		"web.fallback.seed_depth",
		environmentNodeConsumer,
	)
	addEnvironmentSetting(
		add,
		envWebFallbackSeedMaxPage,
		"web.fallback.seed_max_pages",
		environmentNodeConsumer,
	)
}

func addRuntimeEnvironmentFacilities(add environmentCatalogAdder) {
	for environment, surface := range map[string]string{
		envCrawlRPCAddr: bindKeyCrawler,
		envPeerAddr:     bindKeyPeer,
		envPublicAddr:   bindKeyPublic,
		envOpsAddr:      bindKeyOps,
	} {
		add(environment, environmentControlCatalogEntry{
			class: environmentDedicatedAdmin, consumers: environmentNodeConsumer,
			targetSpecificFacility: environment == envCrawlRPCAddr,
			dedicatedAdminSurface:  surface,
		})
	}
	for _, environment := range []string{envAdminUser, envAdminPassword, envSearchAccessToken} {
		add(environment, environmentControlCatalogEntry{
			class: environmentDedicatedAdmin, consumers: environmentNodeConsumer,
			bootstrapCredential: true,
		})
	}
	for _, name := range []string{envPeerHash, envPeerBirthDate} {
		add(
			name,
			environmentControlCatalogEntry{
				class:     environmentBootstrapIdentity,
				consumers: environmentNodeConsumer,
			},
		)
	}
	add(
		"YAGO_CRAWLER_WORKER_ID",
		environmentControlCatalogEntry{
			class:     environmentBootstrapIdentity,
			consumers: environmentCrawlerConsumer,
		},
	)
	add(envDataDir, environmentControlCatalogEntry{
		class:                  environmentHostFacility,
		consumers:              environmentNodeConsumer | environmentCrawlerConsumer,
		targetSpecificFacility: true,
	})
	add("YAGO_CRAWLER_NODE_RPC_ADDR", environmentControlCatalogEntry{
		class:                  environmentBootstrapIdentity,
		consumers:              environmentCrawlerConsumer,
		targetSpecificFacility: true,
	})
}

func addMigrationEnvironmentControls(
	t *testing.T,
	entries map[string]environmentControlCatalogEntry,
	add environmentCatalogAdder,
) {
	t.Helper()
	for _, name := range []string{envWebFallbackEnabled, envWebFallbackProvider, envWebFallbackTrigger} {
		add(
			name,
			environmentControlCatalogEntry{
				class:     environmentMigrationOnly,
				consumers: environmentNodeConsumer,
			},
		)
	}
	for _, legacy := range legacyNodeEnvironmentAliases {
		if _, duplicate := entries[legacy]; duplicate {
			t.Fatalf("legacy environment alias %s duplicates a canonical control", legacy)
		}
		entries[legacy] = environmentControlCatalogEntry{
			class:     environmentMigrationOnly,
			consumers: environmentNodeConsumer,
		}
	}
}

func discoverRuntimeEnvironmentControls(t *testing.T) map[string]struct{} {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	discovered := make(map[string]struct{})
	for _, directory := range []string{filepath.Join(root, "yagonode"), filepath.Join(root, "yago-crawler")} {
		discoverEnvironmentControlsInDirectory(t, directory, discovered)
	}
	for canonical, legacy := range legacyNodeEnvironmentAliases {
		discovered[canonical] = struct{}{}
		discovered[legacy] = struct{}{}
	}
	return discovered
}

func discoverEnvironmentControlsInDirectory(
	t *testing.T,
	directory string,
	discovered map[string]struct{},
) {
	t.Helper()
	err := filepath.WalkDir(directory, environmentSourceVisitor(t, discovered))
	if err != nil {
		t.Fatalf("scan runtime environment controls under %s: %v", directory, err)
	}
}

func environmentSourceVisitor(
	t *testing.T,
	discovered map[string]struct{},
) fs.WalkDirFunc {
	t.Helper()
	return func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "test" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		for name := range environmentControlsInFile(t, path) {
			discovered[name] = struct{}{}
		}

		return nil
	}
}

func environmentControlsInFile(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	bindings := environmentStringBindings(parsed)
	discovered := discoveredEnvironmentBindings(bindings)
	discoverEnvironmentExpressions(parsed, bindings, discovered)

	return discovered
}

func environmentStringBindings(parsed *ast.File) map[string]string {
	bindings := make(map[string]string)
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.GenDecl)
		if !ok || declaration.Tok != token.CONST && declaration.Tok != token.VAR {
			return true
		}
		for _, specification := range declaration.Specs {
			addEnvironmentBindingsFromSpecification(bindings, specification)
		}
		return true
	})

	return bindings
}

func addEnvironmentBindingsFromSpecification(bindings map[string]string, specification ast.Spec) {
	values, ok := specification.(*ast.ValueSpec)
	if !ok {
		return
	}
	for index, name := range values.Names {
		if index >= len(values.Values) {
			continue
		}
		expression := values.Values[index]
		if !isDirectStringBinding(expression) &&
			!environmentStringMayNameControl(expression, bindings) {
			continue
		}
		if value, resolved := evaluateEnvironmentString(expression, bindings); resolved {
			bindings[name.Name] = value
		}
	}
}

func isDirectStringBinding(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.BasicLit:
		return value.Kind == token.STRING
	case *ast.ParenExpr:
		return isDirectStringBinding(value.X)
	default:
		return false
	}
}

func environmentStringMayNameControl(expression ast.Expr, bindings map[string]string) bool {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return false
		}
		unquoted, err := strconv.Unquote(value.Value)
		return err == nil && environmentControlPrefixPossible(unquoted)
	case *ast.BinaryExpr:
		return value.Op == token.ADD && environmentStringMayNameControl(value.X, bindings)
	case *ast.ParenExpr:
		return environmentStringMayNameControl(value.X, bindings)
	case *ast.Ident:
		resolved, found := bindings[value.Name]
		return found && environmentControlPrefixPossible(resolved)
	default:
		return false
	}
}

func environmentControlPrefixPossible(value string) bool {
	for _, prefix := range []string{"YAGO_", "YACY_", "LOG_LEVEL"} {
		if strings.HasPrefix(prefix, value) || strings.HasPrefix(value, prefix) {
			return true
		}
	}

	return false
}

func discoveredEnvironmentBindings(bindings map[string]string) map[string]struct{} {
	discovered := make(map[string]struct{})
	for _, value := range bindings {
		if isEnvironmentControlName(value) {
			discovered[value] = struct{}{}
		}
	}

	return discovered
}

func discoverEnvironmentExpressions(
	parsed *ast.File,
	bindings map[string]string,
	discovered map[string]struct{},
) {
	ast.Inspect(parsed, func(node ast.Node) bool {
		expression, ok := node.(*ast.BinaryExpr)
		if ok && expression.Op == token.ADD {
			if environmentStringMayNameControl(expression, bindings) {
				value, resolved := evaluateEnvironmentString(expression, bindings)
				if resolved && isEnvironmentControlName(value) {
					discovered[value] = struct{}{}
				}
			}
			return false
		}
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, resolved := evaluateEnvironmentString(literal, bindings)
		if resolved && isEnvironmentControlName(value) {
			discovered[value] = struct{}{}
		}
		return true
	})
}

func evaluateEnvironmentString(expression ast.Expr, bindings map[string]string) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		unquoted, err := strconv.Unquote(value.Value)
		return unquoted, err == nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := evaluateEnvironmentString(value.X, bindings)
		right, rightOK := evaluateEnvironmentString(value.Y, bindings)
		return left + right, leftOK && rightOK
	case *ast.ParenExpr:
		return evaluateEnvironmentString(value.X, bindings)
	case *ast.Ident:
		resolved, found := bindings[value.Name]
		return resolved, found
	default:
		return "", false
	}
}

func isEnvironmentControlName(value string) bool {
	if value == "LOG_LEVEL" {
		return true
	}
	if !strings.HasPrefix(value, "YAGO_") && !strings.HasPrefix(value, "YACY_") {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func readEnvironmentAssignments(t *testing.T, source fs.FS, path string) map[string]string {
	t.Helper()
	data, err := fs.ReadFile(source, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	assignments := make(map[string]string)
	for lineNumber, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, found := strings.Cut(trimmed, "=")
		if !found ||
			!isEnvironmentControlName(name) && name != "SOURCE_REVISION" && name != "VERSION" {
			continue
		}
		if _, duplicate := assignments[name]; duplicate {
			t.Fatalf("%s:%d duplicates %s", path, lineNumber+1, name)
		}
		assignments[name] = value
	}
	return assignments
}

func readComposeEnvironment(t *testing.T, source fs.FS, service string) map[string]string {
	t.Helper()
	data, err := fs.ReadFile(source, "docker-compose.yml.example")
	if err != nil {
		t.Fatalf("read docker-compose.yml.example: %v", err)
	}
	assignments := make(map[string]string)
	serviceLines := composeServiceSourceLines(string(data), service)
	for _, line := range composeEnvironmentSourceLines(serviceLines) {
		name, value, found := composeEnvironmentAssignment(line.text)
		if !found {
			continue
		}
		if _, duplicate := assignments[name]; duplicate {
			t.Fatalf(
				"docker-compose.yml.example:%d duplicates %s for %s",
				line.number,
				name,
				service,
			)
		}
		assignments[name] = composeBootstrapValue(t, name, value)
	}
	return assignments
}

func composeServiceSourceLines(source, service string) []composeSourceLine {
	insideService := false
	lines := make([]composeSourceLine, 0)
	for index, line := range strings.Split(source, "\n") {
		if name, found := composeServiceName(line); found {
			insideService = name == service
			continue
		}
		if insideService {
			lines = append(lines, composeSourceLine{number: index + 1, text: line})
		}
	}

	return lines
}

func composeServiceName(line string) (string, bool) {
	if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasSuffix(trimmed, ":") {
		return "", false
	}

	return strings.TrimSuffix(trimmed, ":"), true
}

func composeEnvironmentSourceLines(lines []composeSourceLine) []composeSourceLine {
	insideEnvironment := false
	environmentLines := make([]composeSourceLine, 0)
	for _, line := range lines {
		if line.text == "    environment:" {
			insideEnvironment = true
			continue
		}
		if insideEnvironment && strings.TrimSpace(line.text) != "" &&
			!strings.HasPrefix(line.text, "      ") {
			insideEnvironment = false
		}
		if insideEnvironment && strings.HasPrefix(line.text, "      ") {
			environmentLines = append(environmentLines, line)
		}
	}

	return environmentLines
}

func composeEnvironmentAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	name, value, found := strings.Cut(trimmed, ":")
	if !found || !isEnvironmentControlName(name) {
		return "", "", false
	}

	return name, strings.TrimSpace(value), true
}

func composeBootstrapValue(t *testing.T, environment, value string) string {
	t.Helper()
	prefix := "${" + environment + ":-"
	if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(value, prefix), "}")
	}
	unquoted, err := strconv.Unquote(value)
	if err == nil {
		return unquoted
	}
	return value
}

func assertKnownDeploymentEnvironment(
	t *testing.T,
	name string,
	assignments map[string]string,
	catalog map[string]environmentControlCatalogEntry,
	additional map[string]struct{},
) {
	t.Helper()
	for environment := range assignments {
		if _, found := catalog[environment]; found {
			continue
		}
		if _, found := additional[environment]; found {
			continue
		}
		t.Errorf("%s exposes unclassified environment control %s", name, environment)
	}
}

func assertEnvironmentAbsent(t *testing.T, environment string, assignments ...map[string]string) {
	t.Helper()
	for _, values := range assignments {
		if _, found := values[environment]; found {
			t.Errorf(
				"migration-only environment control %s appears in a canonical deployment example",
				environment,
			)
		}
	}
}

func (deployment consumerDeploymentEnvironment) assertEnvironmentCoverage(
	t *testing.T,
	environment string,
	rootValue string,
	entry environmentControlCatalogEntry,
) {
	t.Helper()
	composeValue, composeFound := deployment.compose[environment]
	systemdValue, systemdFound := deployment.systemd[environment]
	wanted := entry.consumers&deployment.consumer != 0
	if composeFound != wanted {
		t.Errorf(
			"%s Compose coverage for %s = %t, want %t",
			deployment.name,
			environment,
			composeFound,
			wanted,
		)
	}
	if systemdFound != wanted {
		t.Errorf(
			"%s systemd coverage for %s = %t, want %t",
			deployment.name,
			environment,
			systemdFound,
			wanted,
		)
	}
	if !wanted || entry.targetSpecificFacility {
		return
	}
	if composeValue != rootValue {
		t.Errorf(
			"%s Compose default for %s = %q, root default = %q",
			deployment.name,
			environment,
			composeValue,
			rootValue,
		)
	}
	if systemdValue != rootValue {
		t.Errorf(
			"%s systemd default for %s = %q, root default = %q",
			deployment.name,
			environment,
			systemdValue,
			rootValue,
		)
	}
}
