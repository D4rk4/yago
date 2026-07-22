//go:build e2e

package e2e

import (
	"context"
	"net/netip"
	"testing"

	dockernetwork "github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
)

const externalObserverSubnet = "192.31.196.240/29"

func newExternalObserverNetwork(
	t *testing.T,
	ctx context.Context,
) *testcontainers.DockerNetwork {
	t.Helper()
	ipam := dockernetwork.IPAM{
		Driver: "default",
		Config: []dockernetwork.IPAMConfig{{
			Subnet: netip.MustParsePrefix(externalObserverSubnet),
		}},
	}
	internal := tcnetwork.CustomizeNetworkOption(
		func(req *mobyclient.NetworkCreateOptions) error {
			req.Internal = true
			return nil
		},
	)
	network, err := tcnetwork.New(
		ctx,
		tcnetwork.WithDriver("bridge"),
		tcnetwork.WithIPAM(&ipam),
		withoutNetworkMasquerade(),
		internal,
	)
	if err != nil {
		t.Fatalf("create external observer docker network: %v", err)
	}
	t.Cleanup(func() { _ = network.Remove(context.Background()) })
	return network
}

func withoutNetworkMasquerade() tcnetwork.CustomizeNetworkOption {
	return func(req *mobyclient.NetworkCreateOptions) error {
		if req.Options == nil {
			req.Options = map[string]string{}
		}
		req.Options["com.docker.network.bridge.enable_ip_masquerade"] = "false"
		return nil
	}
}

func containerNetworks(
	probeNetworkName, observerNetworkName, alias string,
) ([]string, map[string][]string) {
	if observerNetworkName == "" {
		return []string{probeNetworkName}, map[string][]string{
			probeNetworkName: {alias},
		}
	}

	return []string{probeNetworkName, observerNetworkName}, map[string][]string{
		observerNetworkName: {alias},
	}
}
