package peerwire

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	schemeHTTP         = "http"
	schemeHTTPS        = "https"
	implicitSecurePort = "443"
)

func ContactEndpoints(
	peer yagomodel.Seed,
	path string,
	preferHTTPS bool,
	answered yagomodel.Optional[Endpoint],
) ([]*url.URL, error) {
	advertised, err := peer.ProtocolEndpoints(path, preferHTTPS)
	if err != nil {
		return nil, fmt.Errorf("select peer contact endpoints: %w", err)
	}
	candidates := make([]*url.URL, 0, 2*len(advertised))
	if preferHTTPS {
		candidates = appendImplicitSecureEndpoints(candidates, peer, path)
	}
	candidates = distinctEndpoints(append(candidates, advertised...))

	return promoteAnsweredEndpoint(candidates, answered, preferHTTPS), nil
}

func appendImplicitSecureEndpoints(
	candidates []*url.URL,
	peer yagomodel.Seed,
	path string,
) []*url.URL {
	for _, host := range peer.AdvertisedHosts() {
		if _, err := netip.ParseAddr(host.String()); err == nil {
			continue
		}
		candidates = append(candidates, &url.URL{
			Scheme: schemeHTTPS,
			Host:   net.JoinHostPort(host.String(), implicitSecurePort),
			Path:   path,
		})
	}

	return candidates
}

func distinctEndpoints(candidates []*url.URL) []*url.URL {
	seen := make(map[Endpoint]struct{}, len(candidates))
	distinct := make([]*url.URL, 0, len(candidates))
	for _, candidate := range candidates {
		endpoint := EndpointAt(candidate)
		if _, duplicate := seen[endpoint]; duplicate {
			continue
		}
		seen[endpoint] = struct{}{}
		distinct = append(distinct, candidate)
	}

	return distinct
}

func promoteAnsweredEndpoint(
	candidates []*url.URL,
	answered yagomodel.Optional[Endpoint],
	preferHTTPS bool,
) []*url.URL {
	remembered, ok := answered.Get()
	if !ok || preferHTTPS && remembered.Scheme != schemeHTTPS {
		return candidates
	}
	index := slices.IndexFunc(candidates, func(candidate *url.URL) bool {
		return EndpointAt(candidate) == remembered
	})
	if index <= 0 {
		return candidates
	}
	promoted := candidates[index]

	return append([]*url.URL{promoted}, slices.Delete(candidates, index, index+1)...)
}
