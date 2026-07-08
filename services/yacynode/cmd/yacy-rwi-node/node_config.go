package main

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	envPeerHash         = "YACY_PEER_HASH"
	envPeerName         = "YACY_PEER_NAME"
	envNetworkName      = "YACY_NETWORK_NAME"
	envPeerAddr         = "YACY_PEER_ADDR"
	envOpsAddr          = "YACY_OPS_ADDR"
	envAdvertiseHost    = "YACY_ADVERTISE_HOST"
	envAdvertisePort    = "YACY_ADVERTISE_PORT"
	envDataDir          = "YACY_DATA_DIR"
	envStorageQuota     = "YACY_STORAGE_QUOTA"
	envTrustedProxies   = "YACY_TRUSTED_PROXIES"
	envSeedlistURLs     = "YACY_SEEDLIST_URLS"
	envAnnounceInterval = "YACY_ANNOUNCE_INTERVAL"
	envGreetsPerCycle   = "YACY_GREETS_PER_CYCLE"

	defaultPeerAddr         = ":8090"
	defaultOpsAddr          = ":9090"
	defaultDataDir          = "./data"
	defaultQuota            = "1GB"
	defaultAnnounceInterval = 10 * time.Minute
	defaultGreetsPerCycle   = 16

	storageFileName = "yacy-rwi.db"
)

type nodeConfig struct {
	Hash             yacymodel.Hash
	NetworkName      string
	Name             string
	AdvertiseHost    string
	AdvertisePort    int
	Flags            yacymodel.Flags
	PeerAddr         string
	OpsAddr          string
	StoragePath      string
	StorageQuotaByte int64
	TrustedProxies   []*net.IPNet
	ProxyURL         *url.URL
	SeedlistURLs     []string
	AnnounceInterval time.Duration
	GreetsPerCycle   int
	Crawl            crawlConfig
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

	greetsPerCycle, err := positiveInt(
		envGreetsPerCycle,
		envWithDefault(getenv, envGreetsPerCycle, strconv.Itoa(defaultGreetsPerCycle)),
	)
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

	quota, err := parseByteSize(envWithDefault(getenv, envStorageQuota, defaultQuota))
	if err != nil {
		return nodeConfig{}, fmt.Errorf("%s: %w", envStorageQuota, err)
	}

	proxies, err := parseTrustedProxies(getenv(envTrustedProxies))
	if err != nil {
		return nodeConfig{}, fmt.Errorf("%s: %w", envTrustedProxies, err)
	}

	proxyURL, err := egressProxyURL(getenv)
	if err != nil {
		return nodeConfig{}, err
	}

	dataDir := envWithDefault(getenv, envDataDir, defaultDataDir)

	return nodeConfig{
		Hash:             hash,
		NetworkName:      envWithDefault(getenv, envNetworkName, yacyproto.DefaultNetwork),
		Name:             name,
		AdvertiseHost:    host,
		AdvertisePort:    port,
		Flags:            seniorFlags(),
		PeerAddr:         peerAddr,
		OpsAddr:          envWithDefault(getenv, envOpsAddr, defaultOpsAddr),
		StoragePath:      filepath.Join(dataDir, storageFileName),
		StorageQuotaByte: quota,
		TrustedProxies:   proxies,
		ProxyURL:         proxyURL,
		SeedlistURLs:     seedlistURLs,
		AnnounceInterval: announceInterval,
		GreetsPerCycle:   greetsPerCycle,
	}, nil
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
