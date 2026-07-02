package yagonode

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	envPeerHash          = "YACY_PEER_HASH"
	envPeerName          = "YACY_PEER_NAME"
	envNetworkName       = "YACY_NETWORK_NAME"
	envPeerAddr          = "YACY_PEER_ADDR"
	envOpsAddr           = "YACY_OPS_ADDR"
	envAdvertiseHost     = "YACY_ADVERTISE_HOST"
	envAdvertisePort     = "YACY_ADVERTISE_PORT"
	envPublicSelfTestURL = "YACY_PUBLIC_SELF_TEST_URL"
	envDataDir           = "YACY_DATA_DIR"
	envStorageQuota      = "YACY_STORAGE_QUOTA"
	envTrustedProxies    = "YACY_TRUSTED_PROXIES"
	envSeedlistURLs      = "YACY_SEEDLIST_URLS"
	envAnnounceInterval  = "YACY_ANNOUNCE_INTERVAL"
	envGreetsPerCycle    = "YACY_GREETS_PER_CYCLE"

	defaultPeerAddr         = ":8090"
	defaultOpsAddr          = ":9090"
	defaultDataDir          = "./data"
	defaultQuota            = "1GB"
	defaultAnnounceInterval = 10 * time.Minute
	defaultGreetsPerCycle   = 16

	storageFileName       = "yago-node.db"
	legacyStorageFileName = "yacy-rwi.db"
)

type nodeConfig struct {
	Hash              yacymodel.Hash
	NetworkName       string
	Name              string
	DataDir           string
	AdvertiseHost     string
	AdvertisePort     int
	PublicSelfTestURL *url.URL
	Flags             yacymodel.Flags
	PeerAddr          string
	OpsAddr           string
	StoragePath       string
	StorageQuotaByte  int64
	TrustedProxies    []*net.IPNet
	ProxyURL          *url.URL
	SeedlistURLs      []string
	AnnounceInterval  time.Duration
	GreetsPerCycle    int
	Crawl             crawlConfig
	DHT               dhtDistributionConfig
}

type configuredNodeData struct {
	directory    string
	databasePath string
	quotaByte    int64
}

func loadNodeConfig(getenv func(string) string) (nodeConfig, error) {
	hash, err := yacymodel.ParseHash(strings.TrimSpace(getenv(envPeerHash)))
	if err != nil {
		return nodeConfig{}, fmt.Errorf("%s: %w", envPeerHash, err)
	}

	name, err := requiredEnv(getenv, envPeerName)
	if err != nil {
		return nodeConfig{}, err
	}

	peerAddr := envWithDefault(getenv, envPeerAddr, defaultPeerAddr)

	seedlistURLs := splitList(getenv(envSeedlistURLs))

	announceInterval, err := announceInterval(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	greetsPerCycle, err := greetsPerCycle(getenv)
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

	proxies, err := parseTrustedProxies(getenv(envTrustedProxies))
	if err != nil {
		return nodeConfig{}, fmt.Errorf("%s: %w", envTrustedProxies, err)
	}

	proxyURL, err := egressProxyURL(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	dht, err := loadDHTDistributionConfig(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	return nodeConfig{
		Hash:              hash,
		NetworkName:       envWithDefault(getenv, envNetworkName, yacyproto.DefaultNetwork),
		Name:              name,
		DataDir:           data.directory,
		AdvertiseHost:     host,
		AdvertisePort:     port,
		PublicSelfTestURL: selfTestURL,
		Flags:             seniorFlags(),
		PeerAddr:          peerAddr,
		OpsAddr:           envWithDefault(getenv, envOpsAddr, defaultOpsAddr),
		StoragePath:       data.databasePath,
		StorageQuotaByte:  data.quotaByte,
		TrustedProxies:    proxies,
		ProxyURL:          proxyURL,
		SeedlistURLs:      seedlistURLs,
		AnnounceInterval:  announceInterval,
		GreetsPerCycle:    greetsPerCycle,
		DHT:               dht,
	}, nil
}

func loadConfiguredNodeData(getenv func(string) string) (configuredNodeData, error) {
	directory := envWithDefault(getenv, envDataDir, defaultDataDir)
	quota, err := parseByteSize(envWithDefault(getenv, envStorageQuota, defaultQuota))
	if err != nil {
		return configuredNodeData{}, fmt.Errorf("%s: %w", envStorageQuota, err)
	}

	return configuredNodeData{
		directory:    directory,
		databasePath: configuredDatabasePath(directory),
		quotaByte:    quota,
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

func seniorFlags() yacymodel.Flags {
	flags := yacymodel.ZeroFlags()
	flags = flags.Set(yacymodel.FlagDirectConnect, true)
	flags = flags.Set(yacymodel.FlagAcceptRemoteIndex, true)

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
