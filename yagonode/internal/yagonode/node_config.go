package yagonode

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	envPeerHash            = "YAGO_PEER_HASH"
	envPeerName            = "YAGO_PEER_NAME"
	envNetworkName         = "YAGO_NETWORK_NAME"
	envPeerAddr            = "YAGO_PEER_ADDR"
	envOpsAddr             = "YAGO_OPS_ADDR"
	envPublicAddr          = "YAGO_PUBLIC_ADDR"
	envAdvertiseHost       = "YAGO_ADVERTISE_HOST"
	envAdvertisePort       = "YAGO_ADVERTISE_PORT"
	envPublicSelfTestURL   = "YAGO_PUBLIC_SELF_TEST_URL"
	envDataDir             = "YAGO_DATA_DIR"
	envStorageQuota        = "YAGO_STORAGE_QUOTA"
	envStorageCompaction   = "YAGO_STORAGE_COMPACTION_INTERVAL"
	envStorageAutosplit    = "YAGO_STORAGE_AUTOSPLIT"
	envStorageDeferFsync   = "YAGO_STORAGE_DEFER_FSYNC"
	envStorageReadDefer    = "YAGO_STORAGE_READ_DEFER"
	envTrustedProxies      = "YAGO_TRUSTED_PROXIES"
	envEgressAllowLAN      = "YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS"
	envSeedlistURLs        = "YAGO_SEEDLIST_URLS"
	envAnnounceInterval    = "YAGO_ANNOUNCE_INTERVAL"
	envGreetsPerCycle      = "YAGO_GREETS_PER_CYCLE"
	envSearchAccessToken   = "YAGO_SEARCH_API" + "_KEY"
	envSearchRequireAPIKey = "YAGO_SEARCH_REQUIRE_API" + "_KEY"
	envPublicSearchUI      = "YAGO_PUBLIC_SEARCH_UI_ENABLED"
	envHTTPSRedirect       = "YAGO_HTTPS_REDIRECT"
	envPublicBaseURL       = "YAGO_PUBLIC_BASE_URL"
	envQueryLogMode        = "YAGO_QUERY_LOG_MODE"
	envPeerBirthDate       = "YAGO_PEER_BIRTH_DATE"
	envMetricsEnabled      = "YAGO_METRICS_ENABLED"
	envAdminRestartEnabled = "YAGO_ADMIN_RESTART_ENABLED"
	envIndexRemoteResults  = "YAGO_INDEX_REMOTE_RESULTS"
	envPeerHTTPSPreferred  = "YAGO_PEER_HTTPS_PREFERRED"
	// Seed capability flags advertised to the swarm (YaCy Seed.java flag bits).
	envAdvertiseDirect      = "YAGO_PEER_ADVERTISE_DIRECT"
	envAdvertiseRemoteIndex = "YAGO_PEER_ADVERTISE_REMOTE_INDEX"
	envAdvertiseRootNode    = "YAGO_PEER_ADVERTISE_ROOT_NODE"
	envAdvertiseSSL         = "YAGO_PEER_ADVERTISE_SSL"
	envSearchLinksNewTab    = "YAGO_SEARCH_LINKS_NEW_TAB"
	envSearchClickCapture   = "YAGO_SEARCH_CLICK_CAPTURE"
	envSwarmSeedCrawl       = "YAGO_SWARM_SEED_CRAWL"
	envSwarmSeedDepth       = "YAGO_SWARM_SEED_DEPTH"
	envSwarmSeedMaxPages    = "YAGO_SWARM_SEED_MAX_PAGES"
	envSwarmMorphology      = "YAGO_SWARM_MORPHOLOGY"
	envIngestQualityGate    = "YAGO_INGEST_QUALITY_GATE"
	envPeerSnippetFetch     = "YAGO_PEER_SNIPPET_FETCH"
	envRemotePeerTimeout    = "YAGO_SEARCH_REMOTE_PEER_TIMEOUT"
	envLANDiscovery         = "YAGO_LAN_DISCOVERY"
	envRemoteTimeout        = "YAGO_SEARCH_REMOTE_TIMEOUT"

	defaultPeerAddr         = ":8090"
	defaultOpsAddr          = ":9090"
	defaultPublicAddr       = ":8080"
	defaultDataDir          = "./data"
	defaultQuota            = "1GB"
	defaultAnnounceInterval = 10 * time.Minute
	defaultGreetsPerCycle   = 16

	defaultStorageCompaction = 24 * time.Hour

	storageFileName       = "yago-node.db"
	legacyStorageFileName = "yacy-rwi.db"
	searchIndexDirName    = "search.bleve"
	peerBirthDateLayout   = "20060102"
)

type nodeConfig struct {
	Hash                yagomodel.Hash
	NetworkName         string
	Name                string
	DataDir             string
	AdvertiseHost       string
	AdvertisePort       int
	AdvertisePortPinned bool
	PublicSelfTestURL   *url.URL
	SelfTestURLPinned   bool
	Flags               yagomodel.Flags
	// Seed capability flags advertised to the swarm (YaCy Seed.java bits). Flags is
	// rebuilt from these by configSeedFlags whenever a runtime override lands.
	AdvertiseDirectConnect bool
	AdvertiseRemoteIndex   bool
	AdvertiseRootNode      bool
	AdvertiseSSLAvailable  bool
	PeerAddr               string
	OpsAddr                string
	PublicAddr             string
	StoragePath            string
	SearchIndexPath        string
	StorageQuotaByte       int64
	StorageCompaction      time.Duration
	StorageReadDefer       time.Duration
	StorageAutosplit       bool
	StorageDeferFsync      bool
	TrustedProxies         []*net.IPNet
	EgressAllowLAN         bool
	EgressAllowedCIDRs     []netip.Prefix
	SeedlistURLs           []string
	AnnounceInterval       time.Duration
	GreetsPerCycle         int
	SearchAPIKey           string
	SearchRequireAPIKey    bool
	PublicSearchUIEnabled  bool
	SearchLinksNewTab      bool
	SearchClickCapture     bool
	HTTPSRedirect          bool
	PublicBaseURL          string
	QueryLogMode           queryLogMode
	MetricsEnabled         bool
	AdminRestartEnabled    bool
	IndexRemoteResults     bool
	SwarmMorphology        bool
	PeerSnippetFetch       bool
	RemotePeerTimeout      time.Duration
	RemoteTimeout          time.Duration
	RobotsPolicy           string
	PortalGreeting         string
	SearchRate             publicratelimit.Tiers
	LANDiscovery           bool
	PeerHTTPSPreferred     bool
	SwarmSeed              swarmSeedConfig
	AutocrawlerCrawl       seedCrawlOptions
	DeclaredBirthDate      time.Time
	Crawl                  crawlConfig
	Admin                  adminConfig
	CrossOrigin            crossOriginConfig
	DHT                    dhtDistributionConfig
	WebFallback            webFallbackConfig
	ExtractFetch           extractFetchConfig
}

type configuredNodeData struct {
	directory       string
	databasePath    string
	searchIndexPath string
	quotaByte       int64
	compaction      time.Duration
	readDefer       time.Duration
}

func loadNodeConfig(getenv func(string) string) (nodeConfig, error) {
	hash, err := optionalPeerHash(getenv)
	if err != nil {
		return nodeConfig{}, err
	}
	peerAddr := envWithDefault(getenv, envPeerAddr, defaultPeerAddr)
	seedlistURLs := splitList(getenv(envSeedlistURLs))
	announceInterval, greetsPerCycle, err := announceCadence(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	adv, err := loadPeerAdvertisement(getenv, peerAddr, len(seedlistURLs) > 0)
	if err != nil {
		return nodeConfig{}, err
	}

	data, err := loadConfiguredNodeData(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	proxies, egressAllowLAN, egressAllowedCIDRs, err := egressConfig(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	derived, err := loadDerivedConfigs(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	return nodeConfig{
		Hash:                  hash,
		NetworkName:           envWithDefault(getenv, envNetworkName, yagoproto.DefaultNetwork),
		Name:                  strings.TrimSpace(getenv(envPeerName)),
		DataDir:               data.directory,
		AdvertiseHost:         adv.host,
		AdvertisePort:         adv.port,
		AdvertisePortPinned:   adv.portPinned,
		PublicSelfTestURL:     adv.selfTestURL,
		SelfTestURLPinned:     adv.selfTestPinned,
		PeerAddr:              peerAddr,
		OpsAddr:               envWithDefault(getenv, envOpsAddr, defaultOpsAddr),
		PublicAddr:            publicListenerAddr(getenv),
		StoragePath:           data.databasePath,
		SearchIndexPath:       data.searchIndexPath,
		StorageQuotaByte:      data.quotaByte,
		StorageCompaction:     data.compaction,
		StorageReadDefer:      data.readDefer,
		StorageAutosplit:      derived.storageAutosplit,
		StorageDeferFsync:     derived.storageDeferFsync,
		TrustedProxies:        proxies,
		EgressAllowLAN:        egressAllowLAN,
		EgressAllowedCIDRs:    egressAllowedCIDRs,
		SeedlistURLs:          seedlistURLs,
		AnnounceInterval:      announceInterval,
		GreetsPerCycle:        greetsPerCycle,
		SearchAPIKey:          strings.TrimSpace(getenv(envSearchAccessToken)),
		SearchRequireAPIKey:   derived.requireAPIKey,
		PublicSearchUIEnabled: derived.publicSearchUI,
		SearchLinksNewTab:     derived.searchLinksNewTab,
		SearchClickCapture:    derived.searchClickCapture,
		HTTPSRedirect:         derived.httpsRedirect,
		PublicBaseURL:         derived.publicBaseURL,
		QueryLogMode:          derived.queryLogMode,
		MetricsEnabled:        derived.metricsEnabled,
		AdminRestartEnabled:   derived.adminRestartEnabled,
		IndexRemoteResults:    derived.indexRemoteResults,
		SwarmMorphology:       derived.swarmMorphology,
		PeerSnippetFetch:      derived.peerSnippetFetch,
		RemotePeerTimeout:     derived.remotePeerTimeout,
		RemoteTimeout:         derived.remoteTimeout,
		PeerHTTPSPreferred:    derived.peerHTTPSPreferred,
		SwarmSeed:             derived.swarmSeed,
		AutocrawlerCrawl:      defaultSeedCrawlOptions(),
		DeclaredBirthDate:     derived.birthDate,
		DHT:                   derived.dht,
		WebFallback:           derived.webFallback,
		ExtractFetch:          derived.extractFetch,
	}.withCapabilities(getenv)
}

// withCapabilities loads the operator's seed capability toggles from the
// environment and stamps the resulting advertisement flags onto the config. It
// keeps loadNodeConfig within its length budget while colocating the flag load
// with the fields it fills.
func (c nodeConfig) withCapabilities(
	getenv func(string) string,
) (nodeConfig, error) {
	caps, err := loadSeedCapabilities(getenv)
	if err != nil {
		return nodeConfig{}, err
	}
	c.Flags = caps.flags()
	c.AdvertiseDirectConnect = caps.directConnect
	c.AdvertiseRemoteIndex = caps.remoteIndex
	c.AdvertiseRootNode = caps.rootNode
	c.AdvertiseSSLAvailable = caps.sslAvailable
	return c, nil
}

type derivedConfigs struct {
	dht                 dhtDistributionConfig
	webFallback         webFallbackConfig
	birthDate           time.Time
	requireAPIKey       bool
	publicSearchUI      bool
	httpsRedirect       bool
	publicBaseURL       string
	queryLogMode        queryLogMode
	metricsEnabled      bool
	adminRestartEnabled bool
	indexRemoteResults  bool
	swarmMorphology     bool
	peerSnippetFetch    bool
	peerHTTPSPreferred  bool
	searchLinksNewTab   bool
	searchClickCapture  bool
	storageAutosplit    bool
	storageDeferFsync   bool
	swarmSeed           swarmSeedConfig
	extractFetch        extractFetchConfig
	remotePeerTimeout   time.Duration
	remoteTimeout       time.Duration
}

// swarmSeedConfig gates YaCy-style greedy learning: bounded crawls of URLs
// surfaced by swarm search (YaCy greedylearning.enabled). Seeding has no
// document-count ceiling — a large index must keep discovering resources
// neither it nor the swarm already holds, so growth never self-throttles.
// SeedDepth and SeedMaxPages tune how far each surfaced URL is crawled — the
// autocrawler profile — mirroring the web-fallback seed knobs so both discovery
// paths are equally tunable instead of the swarm path being hardcoded.
type swarmSeedConfig struct {
	Enabled      bool
	SeedDepth    int
	SeedMaxPages int
}

const (
	defaultSwarmSeedDepth    = 5
	defaultSwarmSeedMaxPages = 250
	maxSwarmSeedDepth        = 8
)

type seedCrawlOptions struct {
	AllowQueryURLs      bool
	IgnoreTLSAuthority  bool
	IgnoreRobots        bool
	DisableBrowser      bool
	FollowNoFollowLinks bool
	// RecrawlInterval is how old an indexed page may get before the autocrawler
	// re-fetches it; a zero interval leaves seeded URLs fetched once forever.
	RecrawlInterval time.Duration
}

func defaultSeedCrawlOptions() seedCrawlOptions {
	return seedCrawlOptions{
		AllowQueryURLs:     true,
		IgnoreTLSAuthority: true,
		DisableBrowser:     true,
		RecrawlInterval:    yagocrawlcontract.DefaultRecrawlInterval,
	}
}

func loadSwarmSeedConfig(getenv func(string) string) (swarmSeedConfig, error) {
	enabled, err := boolEnv(getenv, envSwarmSeedCrawl, true)
	if err != nil {
		return swarmSeedConfig{}, fmt.Errorf("%s: %w", envSwarmSeedCrawl, err)
	}
	depth, err := intRangeEnv(
		getenv,
		envSwarmSeedDepth,
		defaultSwarmSeedDepth,
		0,
		maxSwarmSeedDepth,
	)
	if err != nil {
		return swarmSeedConfig{}, fmt.Errorf("%s: %w", envSwarmSeedDepth, err)
	}
	maxPages, err := intAtLeastEnv(getenv, envSwarmSeedMaxPages, defaultSwarmSeedMaxPages, 1)
	if err != nil {
		return swarmSeedConfig{}, fmt.Errorf("%s: %w", envSwarmSeedMaxPages, err)
	}

	return swarmSeedConfig{
		Enabled:      enabled,
		SeedDepth:    depth,
		SeedMaxPages: maxPages,
	}, nil
}

func loadDerivedConfigs(getenv func(string) string) (derivedConfigs, error) {
	dht, err := loadDHTDistributionConfig(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}
	webFallback, err := loadWebFallbackConfig(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}
	birthDate, err := declaredBirthDate(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}
	publicBaseURL, err := normalizePublicBaseURL(getenv(envPublicBaseURL))
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envPublicBaseURL, err)
	}
	queryLog, err := parseQueryLogMode(getenv(envQueryLogMode))
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envQueryLogMode, err)
	}
	extractFetch, err := loadExtractFetchConfig(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}
	swarmSeed, err := loadSwarmSeedConfig(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}
	remotePeerTimeout, err := durationEnv(
		getenv, envRemotePeerTimeout, searchremote.DefaultPerPeerTimeout,
	)
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envRemotePeerTimeout, err)
	}
	remoteTimeout, err := durationEnv(getenv, envRemoteTimeout, searchremote.DefaultOverallTimeout)
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envRemoteTimeout, err)
	}
	toggles, err := loadDerivedBoolToggles(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}

	return derivedConfigs{
		dht:                 dht,
		webFallback:         webFallback,
		birthDate:           birthDate,
		requireAPIKey:       toggles.requireAPIKey,
		publicSearchUI:      toggles.publicSearchUI,
		httpsRedirect:       toggles.httpsRedirect,
		publicBaseURL:       publicBaseURL,
		queryLogMode:        queryLog,
		metricsEnabled:      toggles.metricsEnabled,
		adminRestartEnabled: toggles.adminRestartEnabled,
		indexRemoteResults:  toggles.indexRemoteResults,
		swarmMorphology:     toggles.swarmMorphology,
		peerSnippetFetch:    toggles.peerSnippetFetch,
		peerHTTPSPreferred:  toggles.peerHTTPSPreferred,
		searchLinksNewTab:   toggles.searchLinksNewTab,
		searchClickCapture:  toggles.searchClickCapture,
		storageAutosplit:    toggles.storageAutosplit,
		storageDeferFsync:   toggles.storageDeferFsync,
		swarmSeed:           swarmSeed,
		extractFetch:        extractFetch,
		remotePeerTimeout:   remotePeerTimeout,
		remoteTimeout:       remoteTimeout,
	}, nil
}

// derivedBoolToggles groups the plain boolean feature switches read from the
// environment, keeping loadDerivedConfigs within its length budget.
type derivedBoolToggles struct {
	requireAPIKey       bool
	publicSearchUI      bool
	httpsRedirect       bool
	metricsEnabled      bool
	adminRestartEnabled bool
	indexRemoteResults  bool
	peerHTTPSPreferred  bool
	swarmMorphology     bool
	searchLinksNewTab   bool
	searchClickCapture  bool
	peerSnippetFetch    bool
	storageAutosplit    bool
	storageDeferFsync   bool
}

func loadDerivedBoolToggles(getenv func(string) string) (derivedBoolToggles, error) {
	requireAPIKey, err := boolEnv(getenv, envSearchRequireAPIKey, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envSearchRequireAPIKey, err)
	}
	publicSearchUI, err := boolEnv(getenv, envPublicSearchUI, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envPublicSearchUI, err)
	}
	httpsRedirect, err := boolEnv(getenv, envHTTPSRedirect, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envHTTPSRedirect, err)
	}
	metricsEnabled, err := boolEnv(getenv, envMetricsEnabled, true)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envMetricsEnabled, err)
	}
	adminRestartEnabled, err := boolEnv(getenv, envAdminRestartEnabled, true)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envAdminRestartEnabled, err)
	}
	indexRemoteResults, err := boolEnv(getenv, envIndexRemoteResults, true)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envIndexRemoteResults, err)
	}
	// Default matches YaCy's network.unit.protocol.https.preferred=false: peers
	// advertising the SSL flag get https-first with a plain-http retry when on.
	peerHTTPSPreferred, err := boolEnv(getenv, envPeerHTTPSPreferred, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envPeerHTTPSPreferred, err)
	}
	// Off by default: expanding a single-word swarm query into surface variants
	// multiplies the peer fan-out, so operators opt in when recall matters more
	// than round-trips (ADR-0027).
	swarmMorphology, err := boolEnv(getenv, envSwarmMorphology, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envSwarmMorphology, err)
	}
	// On by default: a peer sends only a result's title, so without loading the
	// page the SERP cannot show the query words the peer matched in the body
	// (YaCy TextSnippet parity); operators on constrained egress can opt out.
	peerSnippetFetch, err := boolEnv(getenv, envPeerSnippetFetch, true)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envPeerSnippetFetch, err)
	}
	// Same-tab is the default per NN/G guidance; opening results in a new tab is
	// an operator opt-in and renders an accessible new-tab indicator.
	searchLinksNewTab, err := boolEnv(getenv, envSearchLinksNewTab, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envSearchLinksNewTab, err)
	}
	// Off by default: capturing result clicks to mine implicit judgments persists
	// query-to-clicked-URL associations, so an operator opts in (YagoRank RANK-00b).
	searchClickCapture, err := boolEnv(getenv, envSearchClickCapture, false)
	if err != nil {
		return derivedBoolToggles{}, fmt.Errorf("%s: %w", envSearchClickCapture, err)
	}
	storageAutosplit, storageDeferFsync, err := loadStorageBoolToggles(getenv)
	if err != nil {
		return derivedBoolToggles{}, err
	}

	return derivedBoolToggles{
		requireAPIKey:       requireAPIKey,
		publicSearchUI:      publicSearchUI,
		httpsRedirect:       httpsRedirect,
		metricsEnabled:      metricsEnabled,
		adminRestartEnabled: adminRestartEnabled,
		indexRemoteResults:  indexRemoteResults,
		peerHTTPSPreferred:  peerHTTPSPreferred,
		swarmMorphology:     swarmMorphology,
		peerSnippetFetch:    peerSnippetFetch,
		searchLinksNewTab:   searchLinksNewTab,
		searchClickCapture:  searchClickCapture,
		storageAutosplit:    storageAutosplit,
		storageDeferFsync:   storageDeferFsync,
	}, nil
}

// loadStorageBoolToggles reads the storage engine's boolean switches: automatic
// shard growth (default on, ADR-0037) and deferred fsync (default off, ADR-0038,
// which trades crash durability for throughput and is only safe where a bounded
// loss window is acceptable).
func loadStorageBoolToggles(getenv func(string) string) (autosplit, deferFsync bool, err error) {
	autosplit, err = boolEnv(getenv, envStorageAutosplit, true)
	if err != nil {
		return false, false, fmt.Errorf("%s: %w", envStorageAutosplit, err)
	}
	deferFsync, err = boolEnv(getenv, envStorageDeferFsync, false)
	if err != nil {
		return false, false, fmt.Errorf("%s: %w", envStorageDeferFsync, err)
	}

	return autosplit, deferFsync, nil
}

func egressConfig(getenv func(string) string) ([]*net.IPNet, bool, []netip.Prefix, error) {
	proxies, err := parseTrustedProxies(getenv(envTrustedProxies))
	if err != nil {
		return nil, false, nil, fmt.Errorf("%s: %w", envTrustedProxies, err)
	}
	allowLAN, err := boolEnv(getenv, envEgressAllowLAN, false)
	if err != nil {
		return nil, false, nil, fmt.Errorf("%s: %w", envEgressAllowLAN, err)
	}
	allowedCIDRs, err := parseEgressAllowCIDRs(getenv(envEgressAllowCIDRs))
	if err != nil {
		return nil, false, nil, fmt.Errorf("%s: %w", envEgressAllowCIDRs, err)
	}

	return proxies, allowLAN, allowedCIDRs, nil
}

func declaredBirthDate(getenv func(string) string) (time.Time, error) {
	raw := strings.TrimSpace(getenv(envPeerBirthDate))
	if raw == "" {
		return time.Time{}, nil
	}

	birth, err := time.ParseInLocation(peerBirthDateLayout, raw, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", envPeerBirthDate, err)
	}

	return birth, nil
}

func loadConfiguredNodeData(getenv func(string) string) (configuredNodeData, error) {
	directory := envWithDefault(getenv, envDataDir, defaultDataDir)
	quota, err := parseByteSize(envWithDefault(getenv, envStorageQuota, defaultQuota))
	if err != nil {
		return configuredNodeData{}, fmt.Errorf("%s: %w", envStorageQuota, err)
	}

	compaction, err := storageCompactionInterval(getenv)
	if err != nil {
		return configuredNodeData{}, err
	}

	readDefer, err := storageReadDeferBudget(getenv)
	if err != nil {
		return configuredNodeData{}, err
	}

	return configuredNodeData{
		directory:       directory,
		databasePath:    configuredDatabasePath(directory),
		searchIndexPath: filepath.Join(directory, searchIndexDirName),
		quotaByte:       quota,
		compaction:      compaction,
		readDefer:       readDefer,
	}, nil
}

func configuredDatabasePath(directory string) string {
	current := filepath.Join(directory, storageFileName)
	if _, err := os.Stat(current); err == nil {
		return current
	}

	legacy := filepath.Join(directory, legacyStorageFileName)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}

	return current
}

func publicSelfTestURL(getenv func(string) string, peerAddr string) (*url.URL, error) {
	raw := strings.TrimSpace(getenv(envPublicSelfTestURL))
	if raw == "" {
		return localSelfTestURL(peerAddr)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envPublicSelfTestURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s: scheme must be http or https", envPublicSelfTestURL)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%s: host must be set", envPublicSelfTestURL)
	}

	return parsed, nil
}

func localSelfTestURL(peerAddr string) (*url.URL, error) {
	host, port, err := net.SplitHostPort(peerAddr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envPeerAddr, err)
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}

	return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}, nil
}

func announceCadence(getenv func(string) string) (time.Duration, int, error) {
	interval, err := announceInterval(getenv)
	if err != nil {
		return 0, 0, err
	}
	greets, err := greetsPerCycle(getenv)
	if err != nil {
		return 0, 0, err
	}

	return interval, greets, nil
}

func greetsPerCycle(getenv func(string) string) (int, error) {
	return positiveInt(
		envGreetsPerCycle,
		envWithDefault(getenv, envGreetsPerCycle, strconv.Itoa(defaultGreetsPerCycle)),
	)
}

// storageCompactionInterval reads how often the storage engine compacts its
// shard files to return freed pages to the OS (ADR-0036 C). It accepts the
// recrawl-interval vocabulary (e.g. 1d, 12h, off); off — or 0 — disables
// compaction, and an empty value keeps the default cadence.
func storageCompactionInterval(getenv func(string) string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(envStorageCompaction))
	if raw == "" {
		return defaultStorageCompaction, nil
	}

	interval, err := yagocrawlcontract.ParseRecrawlInterval(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", envStorageCompaction, err)
	}

	return interval, nil
}

// storageReadDeferBudget reads how long an ingest write yields to in-flight
// interactive reads (IO-PRIO-01 / PERF-PRIO-02). An empty value keeps the engine
// default (readpriority.DefaultBudget); a Go duration tunes it, and a negative
// value disables the yield.
func storageReadDeferBudget(getenv func(string) string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(envStorageReadDefer))
	if raw == "" {
		return 0, nil
	}

	budget, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", envStorageReadDefer, err)
	}

	return budget, nil
}

func announceInterval(getenv func(string) string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(envAnnounceInterval))
	if raw == "" {
		return defaultAnnounceInterval, nil
	}

	interval, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", envAnnounceInterval, err)
	}
	if interval <= 0 {
		return 0, fmt.Errorf("%s: must be positive", envAnnounceInterval)
	}

	return interval, nil
}

func splitList(raw string) []string {
	var out []string
	for item := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// advertiseHost resolves the host advertised to the network. An explicit
// YAGO_ADVERTISE_HOST always wins. Otherwise, when the node announces itself, it
// auto-detects a best-guess address from the machine's interfaces so a node
// bootstraps without the operator pinning one. The guess can be wrong behind
// NAT or Docker (set YAGO_ADVERTISE_HOST then), and the DHT self-test demotes an
// unreachable self; detection never fails, so a node that cannot guess an
// address still starts rather than refusing to boot.
func advertiseHost(getenv func(string) string, announcing bool) string {
	host := strings.TrimSpace(getenv(envAdvertiseHost))
	if host != "" {
		return host
	}
	if !announcing {
		return ""
	}

	return detectAdvertiseHost(net.InterfaceAddrs)
}

// detectAdvertiseHost returns the first non-loopback IPv4 address the machine
// has, or an empty string when the interfaces cannot be read or none qualifies.
func detectAdvertiseHost(addrs func() ([]net.Addr, error)) string {
	found, err := addrs()
	if err != nil {
		return ""
	}
	for _, addr := range found {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip == nil || ip.IsLoopback() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}

	return ""
}

// peerAdvertisement bundles how the node presents its peer endpoint to the
// network: the advertised host and port and the local DHT self-test URL, plus
// whether the port and self-test URL were pinned by their own environment
// variables (so a later peer-bind change does not silently override them).
type peerAdvertisement struct {
	host           string
	port           int
	portPinned     bool
	selfTestURL    *url.URL
	selfTestPinned bool
}

func loadPeerAdvertisement(
	getenv func(string) string,
	peerAddr string,
	announcing bool,
) (peerAdvertisement, error) {
	host := advertiseHost(getenv, announcing)
	port, err := advertisePort(getenv, peerAddr)
	if err != nil {
		return peerAdvertisement{}, err
	}
	selfTestURL, err := publicSelfTestURL(getenv, peerAddr)
	if err != nil {
		return peerAdvertisement{}, err
	}

	return peerAdvertisement{
		host:           host,
		port:           port,
		portPinned:     strings.TrimSpace(getenv(envAdvertisePort)) != "",
		selfTestURL:    selfTestURL,
		selfTestPinned: strings.TrimSpace(getenv(envPublicSelfTestURL)) != "",
	}, nil
}

func advertisePort(getenv func(string) string, peerAddr string) (int, error) {
	if raw := strings.TrimSpace(getenv(envAdvertisePort)); raw != "" {
		return positiveInt(envAdvertisePort, raw)
	}

	_, portPart, err := net.SplitHostPort(peerAddr)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", envPeerAddr, err)
	}

	return positiveInt(envPeerAddr, portPart)
}

// publicListenerAddr resolves the dedicated public search listener's address. It
// defaults to defaultPublicAddr; the sentinels "off", "none", and "disabled"
// (any case) turn the public surface off, returning an empty address that
// suppresses the listener entirely so the node stays a pure peer.
func publicListenerAddr(getenv func(string) string) string {
	raw := strings.TrimSpace(getenv(envPublicAddr))
	if raw == "" {
		return defaultPublicAddr
	}
	switch strings.ToLower(raw) {
	case "off", "none", "disabled":
		return ""
	default:
		return raw
	}
}

// seedCapabilities holds the operator-controlled swarm capability flags this
// node advertises in its seed. The values map onto YaCy Seed.java flag bits.
type seedCapabilities struct {
	directConnect bool
	remoteIndex   bool
	rootNode      bool
	sslAvailable  bool
}

// loadSeedCapabilities reads the advertised swarm capability flags from the
// environment. Defaults preserve the historical advertisement (direct connect
// and accept-remote-index on), leaving root-node and SSL off until an operator
// opts in.
func loadSeedCapabilities(getenv func(string) string) (seedCapabilities, error) {
	directConnect, err := boolEnv(getenv, envAdvertiseDirect, true)
	if err != nil {
		return seedCapabilities{}, fmt.Errorf("%s: %w", envAdvertiseDirect, err)
	}
	remoteIndex, err := boolEnv(getenv, envAdvertiseRemoteIndex, true)
	if err != nil {
		return seedCapabilities{}, fmt.Errorf("%s: %w", envAdvertiseRemoteIndex, err)
	}
	rootNode, err := boolEnv(getenv, envAdvertiseRootNode, false)
	if err != nil {
		return seedCapabilities{}, fmt.Errorf("%s: %w", envAdvertiseRootNode, err)
	}
	sslAvailable, err := boolEnv(getenv, envAdvertiseSSL, false)
	if err != nil {
		return seedCapabilities{}, fmt.Errorf("%s: %w", envAdvertiseSSL, err)
	}

	return seedCapabilities{
		directConnect: directConnect,
		remoteIndex:   remoteIndex,
		rootNode:      rootNode,
		sslAvailable:  sslAvailable,
	}, nil
}

// flags renders the capability set as the YaCy seed flag bitfield.
// FlagAcceptRemoteCrawl deliberately stays clear: remote crawl execution is
// disabled for SSRF safety (see doc/remote-crawl-policy.md), and advertising a
// capability the crawlReceipt endpoint rejects would break the swarm contract.
func (c seedCapabilities) flags() yagomodel.Flags {
	flags := yagomodel.ZeroFlags()
	flags = flags.Set(yagomodel.FlagDirectConnect, c.directConnect)
	flags = flags.Set(yagomodel.FlagAcceptRemoteIndex, c.remoteIndex)
	flags = flags.Set(yagomodel.FlagRootNode, c.rootNode)
	flags = flags.Set(yagomodel.FlagSSLAvailable, c.sslAvailable)

	return flags
}

// configSeedFlags rebuilds the advertised seed flags from a config's capability
// toggles, so a runtime override to any toggle re-derives the bitfield.
func configSeedFlags(config nodeConfig) yagomodel.Flags {
	return seedCapabilities{
		directConnect: config.AdvertiseDirectConnect,
		remoteIndex:   config.AdvertiseRemoteIndex,
		rootNode:      config.AdvertiseRootNode,
		sslAvailable:  config.AdvertiseSSLAvailable,
	}.flags()
}

// optionalPeerHash parses the peer hash from the environment when set. An empty
// value is not an error: the effective identity is resolved (and, if needed,
// generated and persisted) later against the data directory, so a node
// bootstraps without the operator having to supply a hash.
func optionalPeerHash(getenv func(string) string) (yagomodel.Hash, error) {
	raw := strings.TrimSpace(getenv(envPeerHash))
	if raw == "" {
		return "", nil
	}
	hash, err := yagomodel.ParseHash(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", envPeerHash, err)
	}

	return hash, nil
}

func positiveInt(key, raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}

	return value, nil
}

func envWithDefault(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}

	return fallback
}
