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

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	envPeerHash            = "YAGO_PEER_HASH"
	envPeerName            = "YAGO_PEER_NAME"
	envNetworkName         = "YAGO_NETWORK_NAME"
	envPeerAddr            = "YAGO_PEER_ADDR"
	envOpsAddr             = "YAGO_OPS_ADDR"
	envAdvertiseHost       = "YAGO_ADVERTISE_HOST"
	envAdvertisePort       = "YAGO_ADVERTISE_PORT"
	envPublicSelfTestURL   = "YAGO_PUBLIC_SELF_TEST_URL"
	envDataDir             = "YAGO_DATA_DIR"
	envStorageQuota        = "YAGO_STORAGE_QUOTA"
	envTrustedProxies      = "YAGO_TRUSTED_PROXIES"
	envEgressAllowLAN      = "YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS"
	envSeedlistURLs        = "YAGO_SEEDLIST_URLS"
	envAnnounceInterval    = "YAGO_ANNOUNCE_INTERVAL"
	envGreetsPerCycle      = "YAGO_GREETS_PER_CYCLE"
	envSearchAccessToken   = "YAGO_SEARCH_API" + "_KEY"
	envSearchRequireAPIKey = "YAGO_SEARCH_REQUIRE_API" + "_KEY"
	envPublicSearchUI      = "YAGO_PUBLIC_SEARCH_UI_ENABLED"
	envHTTPSRedirect       = "YAGO_HTTPS_REDIRECT"
	envPeerBirthDate       = "YAGO_PEER_BIRTH_DATE"

	defaultPeerAddr         = ":8090"
	defaultOpsAddr          = ":9090"
	defaultDataDir          = "./data"
	defaultQuota            = "1GB"
	defaultAnnounceInterval = 10 * time.Minute
	defaultGreetsPerCycle   = 16

	storageFileName       = "yago-node.db"
	legacyStorageFileName = "yacy-rwi.db"
	searchIndexDirName    = "search.bleve"
	peerBirthDateLayout   = "20060102"
)

type nodeConfig struct {
	Hash                  yagomodel.Hash
	NetworkName           string
	Name                  string
	DataDir               string
	AdvertiseHost         string
	AdvertisePort         int
	PublicSelfTestURL     *url.URL
	Flags                 yagomodel.Flags
	PeerAddr              string
	OpsAddr               string
	StoragePath           string
	SearchIndexPath       string
	StorageQuotaByte      int64
	TrustedProxies        []*net.IPNet
	EgressAllowLAN        bool
	EgressAllowedCIDRs    []netip.Prefix
	SeedlistURLs          []string
	AnnounceInterval      time.Duration
	GreetsPerCycle        int
	SearchAPIKey          string
	SearchRequireAPIKey   bool
	PublicSearchUIEnabled bool
	HTTPSRedirect         bool
	DeclaredBirthDate     time.Time
	Crawl                 crawlConfig
	Admin                 adminConfig
	CrossOrigin           crossOriginConfig
	DHT                   dhtDistributionConfig
	WebFallback           webFallbackConfig
	ExtractFetch          extractFetchConfig
}

type configuredNodeData struct {
	directory       string
	databasePath    string
	searchIndexPath string
	quotaByte       int64
}

func loadNodeConfig(getenv func(string) string) (nodeConfig, error) {
	hash, err := yagomodel.ParseHash(strings.TrimSpace(getenv(envPeerHash)))
	if err != nil {
		return nodeConfig{}, fmt.Errorf("%s: %w", envPeerHash, err)
	}

	name, err := requiredEnv(getenv, envPeerName)
	if err != nil {
		return nodeConfig{}, err
	}

	peerAddr := envWithDefault(getenv, envPeerAddr, defaultPeerAddr)
	seedlistURLs := splitList(getenv(envSeedlistURLs))

	announceInterval, greetsPerCycle, err := announceCadence(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	host, err := advertiseHost(getenv, len(seedlistURLs) > 0)
	if err != nil {
		return nodeConfig{}, err
	}

	port, err := advertisePort(getenv, peerAddr)
	if err != nil {
		return nodeConfig{}, err
	}

	selfTestURL, err := publicSelfTestURL(getenv, peerAddr)
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
		Name:                  name,
		DataDir:               data.directory,
		AdvertiseHost:         host,
		AdvertisePort:         port,
		PublicSelfTestURL:     selfTestURL,
		Flags:                 seniorFlags(),
		PeerAddr:              peerAddr,
		OpsAddr:               envWithDefault(getenv, envOpsAddr, defaultOpsAddr),
		StoragePath:           data.databasePath,
		SearchIndexPath:       data.searchIndexPath,
		StorageQuotaByte:      data.quotaByte,
		TrustedProxies:        proxies,
		EgressAllowLAN:        egressAllowLAN,
		EgressAllowedCIDRs:    egressAllowedCIDRs,
		SeedlistURLs:          seedlistURLs,
		AnnounceInterval:      announceInterval,
		GreetsPerCycle:        greetsPerCycle,
		SearchAPIKey:          strings.TrimSpace(getenv(envSearchAccessToken)),
		SearchRequireAPIKey:   derived.requireAPIKey,
		PublicSearchUIEnabled: derived.publicSearchUI,
		HTTPSRedirect:         derived.httpsRedirect,
		DeclaredBirthDate:     derived.birthDate,
		DHT:                   derived.dht,
		WebFallback:           derived.webFallback,
		ExtractFetch:          derived.extractFetch,
	}, nil
}

type derivedConfigs struct {
	dht            dhtDistributionConfig
	webFallback    webFallbackConfig
	birthDate      time.Time
	requireAPIKey  bool
	publicSearchUI bool
	httpsRedirect  bool
	extractFetch   extractFetchConfig
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
	requireAPIKey, err := boolEnv(getenv, envSearchRequireAPIKey, false)
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envSearchRequireAPIKey, err)
	}
	publicSearchUI, err := boolEnv(getenv, envPublicSearchUI, false)
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envPublicSearchUI, err)
	}
	httpsRedirect, err := boolEnv(getenv, envHTTPSRedirect, false)
	if err != nil {
		return derivedConfigs{}, fmt.Errorf("%s: %w", envHTTPSRedirect, err)
	}
	extractFetch, err := loadExtractFetchConfig(getenv)
	if err != nil {
		return derivedConfigs{}, err
	}

	return derivedConfigs{
		dht:            dht,
		webFallback:    webFallback,
		birthDate:      birthDate,
		requireAPIKey:  requireAPIKey,
		publicSearchUI: publicSearchUI,
		httpsRedirect:  httpsRedirect,
		extractFetch:   extractFetch,
	}, nil
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

	return configuredNodeData{
		directory:       directory,
		databasePath:    configuredDatabasePath(directory),
		searchIndexPath: filepath.Join(directory, searchIndexDirName),
		quotaByte:       quota,
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

func advertiseHost(getenv func(string) string, announcing bool) (string, error) {
	host := strings.TrimSpace(getenv(envAdvertiseHost))
	if host == "" && announcing {
		return "", fmt.Errorf("%s: must be set when announcing to the network", envAdvertiseHost)
	}

	return host, nil
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

func seniorFlags() yagomodel.Flags {
	flags := yagomodel.ZeroFlags()
	flags = flags.Set(yagomodel.FlagDirectConnect, true)
	flags = flags.Set(yagomodel.FlagAcceptRemoteIndex, true)

	return flags
}

func requiredEnv(getenv func(string) string, key string) (string, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s: must be set", key)
	}

	return value, nil
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
