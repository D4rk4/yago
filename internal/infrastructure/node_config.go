package infrastructure

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	EnvPeerHash       = "YACY_PEER_HASH"
	EnvPeerName       = "YACY_PEER_NAME"
	EnvNetworkName    = "YACY_NETWORK_NAME"
	EnvPeerAddr       = "YACY_PEER_ADDR"
	EnvOpsAddr        = "YACY_OPS_ADDR"
	EnvAdvertiseHost  = "YACY_ADVERTISE_HOST"
	EnvAdvertisePort  = "YACY_ADVERTISE_PORT"
	EnvDataDir        = "YACY_DATA_DIR"
	EnvStorageQuota   = "YACY_STORAGE_QUOTA"
	EnvTrustedProxies = "YACY_TRUSTED_PROXIES"

	defaultPeerAddr = ":8090"
	defaultOpsAddr  = ":9090"
	defaultDataDir  = "./data"
	defaultQuota    = "1GB"

	storageFileName = "yacy-rwi.db"
)

type NodeConfig struct {
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
}

func LoadNodeConfig(getenv func(string) string, announcing bool) (NodeConfig, error) {
	hash, err := yacymodel.ParseHash(strings.TrimSpace(getenv(EnvPeerHash)))
	if err != nil {
		return NodeConfig{}, fmt.Errorf("%s: %w", EnvPeerHash, err)
	}

	name, err := required(getenv, EnvPeerName)
	if err != nil {
		return NodeConfig{}, err
	}

	peerAddr := withDefault(getenv, EnvPeerAddr, defaultPeerAddr)

	host, err := advertiseHost(getenv, announcing)
	if err != nil {
		return NodeConfig{}, err
	}

	port, err := advertisePort(getenv, peerAddr)
	if err != nil {
		return NodeConfig{}, err
	}

	quota, err := parseByteSize(withDefault(getenv, EnvStorageQuota, defaultQuota))
	if err != nil {
		return NodeConfig{}, fmt.Errorf("%s: %w", EnvStorageQuota, err)
	}

	proxies, err := parseTrustedProxies(getenv(EnvTrustedProxies))
	if err != nil {
		return NodeConfig{}, fmt.Errorf("%s: %w", EnvTrustedProxies, err)
	}

	dataDir := withDefault(getenv, EnvDataDir, defaultDataDir)

	return NodeConfig{
		Hash:             hash,
		NetworkName:      withDefault(getenv, EnvNetworkName, yacyproto.DefaultNetwork),
		Name:             name,
		AdvertiseHost:    host,
		AdvertisePort:    port,
		Flags:            seniorFlags(),
		PeerAddr:         peerAddr,
		OpsAddr:          withDefault(getenv, EnvOpsAddr, defaultOpsAddr),
		StoragePath:      storagePath(dataDir),
		StorageQuotaByte: quota,
		TrustedProxies:   proxies,
	}, nil
}

func StoragePath(getenv func(string) string) string {
	return storagePath(withDefault(getenv, EnvDataDir, defaultDataDir))
}

func storagePath(dataDir string) string {
	return filepath.Join(dataDir, storageFileName)
}

func advertiseHost(getenv func(string) string, announcing bool) (string, error) {
	host := strings.TrimSpace(getenv(EnvAdvertiseHost))
	if host == "" && announcing {
		return "", fmt.Errorf("%s: must be set when announcing to the network", EnvAdvertiseHost)
	}

	return host, nil
}

func advertisePort(getenv func(string) string, peerAddr string) (int, error) {
	if raw := strings.TrimSpace(getenv(EnvAdvertisePort)); raw != "" {
		return positiveInt(EnvAdvertisePort, raw)
	}

	_, portPart, err := net.SplitHostPort(peerAddr)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", EnvPeerAddr, err)
	}

	return positiveInt(EnvPeerAddr, portPart)
}

func seniorFlags() yacymodel.Flags {
	flags := yacymodel.ZeroFlags()
	flags = flags.Set(yacymodel.FlagDirectConnect, true)
	flags = flags.Set(yacymodel.FlagAcceptRemoteIndex, true)

	return flags
}

func required(getenv func(string) string, key string) (string, error) {
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

func withDefault(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}

	return fallback
}
