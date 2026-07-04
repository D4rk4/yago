package yagonode

import (
	"fmt"
	"net"
	"sort"
	"strconv"
)

// bindAddress is one host address the machine can bind a listener to.
type bindAddress struct {
	host  string
	label string
}

const bindAllInterfacesLabel = "All interfaces"

// bindDefinition describes one bindable surface whose listen address operators
// can override at runtime. guardAdmin marks the admin/ops surface, which carries
// the self-lockout guardrail.
type bindDefinition struct {
	key         string
	title       string
	description string
	guardAdmin  bool
	current     func(config nodeConfig) string
	apply       func(config nodeConfig, addr string) nodeConfig
}

const (
	bindKeyPeer = "bind.peer"
	bindKeyOps  = "bind.ops"
)

func bindDefinitions() []bindDefinition {
	return []bindDefinition{
		{
			key:         bindKeyPeer,
			title:       "Peer protocol (P2P)",
			description: "The public YaCy peer listener; also serves the Tavily API and portal.",
			current:     func(config nodeConfig) string { return config.PeerAddr },
			apply: func(config nodeConfig, addr string) nodeConfig {
				config.PeerAddr = addr

				return config
			},
		},
		{
			key:         bindKeyOps,
			title:       "Admin and ops",
			description: "The operator console, health, readiness, and metrics listener.",
			guardAdmin:  true,
			current:     func(config nodeConfig) string { return config.OpsAddr },
			apply: func(config nodeConfig, addr string) nodeConfig {
				config.OpsAddr = addr

				return config
			},
		},
	}
}

func indexBindDefinitions() map[string]bindDefinition {
	definitions := bindDefinitions()
	byKey := make(map[string]bindDefinition, len(definitions))
	for _, def := range definitions {
		byKey[def.key] = def
	}

	return byKey
}

// applyBindOverrides layers stored bind overrides onto the configuration at
// startup. A malformed override is ignored so the environment address stands.
func applyBindOverrides(config nodeConfig, overrides map[string]string) nodeConfig {
	byKey := indexBindDefinitions()
	for key, raw := range overrides {
		def, ok := byKey[key]
		if !ok {
			continue
		}
		host, port, err := splitBindAddr(raw)
		if err != nil {
			continue
		}
		config = def.apply(config, formatBindAddr(host, port))
	}

	return config
}

// validateNodeBinds rejects malformed peer or ops listen addresses at boot.
func validateNodeBinds(config nodeConfig) error {
	for _, def := range bindDefinitions() {
		addr := def.current(config)
		if _, _, err := splitBindAddr(addr); err != nil {
			return fmt.Errorf("%s bind %q: %w", def.title, addr, err)
		}
	}

	return nil
}

// splitBindAddr parses a "host:port" listen address, allowing an empty host
// (all interfaces). A non-empty host must be an IP literal.
func splitBindAddr(addr string) (host string, port int, err error) {
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("split host and port: %w", err)
	}
	if host != "" && net.ParseIP(host) == nil {
		return "", 0, fmt.Errorf("host %q is not an IP address", host)
	}
	port, err = parseBindPort(rawPort)
	if err != nil {
		return "", 0, err
	}

	return host, port, nil
}

func parseBindPort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", raw)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d is out of range 1-65535", port)
	}

	return port, nil
}

func formatBindAddr(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// discoverBindAddresses lists the machine's bindable host addresses, always
// including the all-interfaces choice and loopback, sorted for stable display.
func discoverBindAddresses(addrs func() ([]net.Addr, error)) ([]bindAddress, error) {
	interfaceAddrs, err := addrs()
	if err != nil {
		return nil, fmt.Errorf("list interface addresses: %w", err)
	}

	seen := map[string]bindAddress{"": {host: "", label: bindAllInterfacesLabel}}
	for _, addr := range interfaceAddrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			continue
		}
		host := ip.String()
		seen[host] = bindAddress{host: host, label: bindAddressLabel(ip)}
	}

	return sortedBindAddresses(seen), nil
}

func bindAddressLabel(ip net.IP) string {
	if ip.IsLoopback() {
		return ip.String() + " (loopback)"
	}

	return ip.String()
}

func sortedBindAddresses(seen map[string]bindAddress) []bindAddress {
	out := make([]bindAddress, 0, len(seen))
	for _, addr := range seen {
		out = append(out, addr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].host == "" || out[j].host == "" {
			return out[i].host == ""
		}

		return out[i].host < out[j].host
	})

	return out
}
