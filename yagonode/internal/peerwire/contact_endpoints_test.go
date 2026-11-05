package peerwire

import (
	"errors"
	"net/url"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const contactPath = "/yacy/hello.html"

type contactAdvertisement struct {
	hosts      []string
	securePort string
}

func contactSeed(t *testing.T, advertisement contactAdvertisement) yagomodel.Seed {
	t.Helper()
	hosts := make([]yagomodel.Host, 0, len(advertisement.hosts))
	for _, raw := range advertisement.hosts {
		host, err := yagomodel.ParseHost(raw)
		if err != nil {
			t.Fatal(err)
		}
		hosts = append(hosts, host)
	}
	port, err := yagomodel.ParsePort("8090")
	if err != nil {
		t.Fatal(err)
	}
	seed := yagomodel.Seed{
		Hash: yagomodel.Hash("ABCDEFGHIJKL"),
		IP:   yagomodel.Some(hosts[0]),
		IP6:  yagomodel.Some(hosts[1:]),
		Port: yagomodel.Some(port),
	}
	if advertisement.securePort != "" {
		securePort, err := yagomodel.ParsePort(advertisement.securePort)
		if err != nil {
			t.Fatal(err)
		}
		seed.PortSSL = yagomodel.Some(securePort)
		seed.Version = yagomodel.Some(yagomodel.YaCyVersion("1.941"))
		seed.Flags = yagomodel.Some(yagomodel.ZeroFlags().Set(yagomodel.FlagSSLAvailable, true))
	}

	return seed
}

func contactOrigins(t *testing.T, endpoints []*url.URL) []string {
	t.Helper()
	origins := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Path != contactPath || endpoint.RawQuery != "" || endpoint.User != nil {
			t.Fatalf("endpoint %s does not carry exactly the protocol path", endpoint)
		}
		origins = append(origins, EndpointAt(endpoint).Origin())
	}

	return origins
}

func selectContactOrigins(
	t *testing.T,
	seed yagomodel.Seed,
	preferHTTPS bool,
	answered yagomodel.Optional[Endpoint],
) []string {
	t.Helper()
	endpoints, err := ContactEndpoints(seed, contactPath, preferHTTPS, answered)
	if err != nil {
		t.Fatal(err)
	}

	return contactOrigins(t, endpoints)
}

func assertContactOrigins(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("contact endpoints = %v, want %v", got, want)
	}
}

func noAnsweredEndpoint() yagomodel.Optional[Endpoint] {
	return yagomodel.None[Endpoint]()
}

func answeredAt(t *testing.T, origin string) yagomodel.Optional[Endpoint] {
	t.Helper()
	endpoint, err := ParseEndpointOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}

	return yagomodel.Some(endpoint)
}

func TestContactEndpointsAddImplicitHTTPSOnlyForDNSNamesUnderPreference(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{
		hosts: []string{"example.org", "192.0.2.7", "2001:db8::7"},
	})

	assertContactOrigins(t, selectContactOrigins(t, seed, true, noAnsweredEndpoint()),
		"https://example.org:443",
		"http://example.org:8090",
		"http://192.0.2.7:8090",
		"http://[2001:db8::7]:8090",
	)
	assertContactOrigins(t, selectContactOrigins(t, seed, false, noAnsweredEndpoint()),
		"http://example.org:8090",
		"http://192.0.2.7:8090",
		"http://[2001:db8::7]:8090",
	)
}

func TestContactEndpointsWithoutPreferenceMatchAdvertisedProtocolEndpoints(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{
		hosts:      []string{"example.org", "192.0.2.7"},
		securePort: "8443",
	})
	advertised, err := seed.ProtocolEndpoints(contactPath, false)
	if err != nil {
		t.Fatal(err)
	}

	assertContactOrigins(t, selectContactOrigins(t, seed, false, noAnsweredEndpoint()),
		contactOrigins(t, advertised)...)
}

func TestContactEndpointsKeepExplicitSecurePortAfterImplicitHTTPS(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{
		hosts:      []string{"example.org", "192.0.2.7"},
		securePort: "8443",
	})

	assertContactOrigins(t, selectContactOrigins(t, seed, true, noAnsweredEndpoint()),
		"https://example.org:443",
		"https://example.org:8443",
		"http://example.org:8090",
		"https://192.0.2.7:8443",
		"http://192.0.2.7:8090",
	)
}

func TestContactEndpointsDeduplicateAdvertisedPort443(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{
		hosts:      []string{"example.org"},
		securePort: "443",
	})

	assertContactOrigins(t, selectContactOrigins(t, seed, true, noAnsweredEndpoint()),
		"https://example.org:443",
		"http://example.org:8090",
	)
}

func TestContactEndpointsPromoteStillAdvertisedAnsweredEndpoint(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{
		hosts:      []string{"example.org", "192.0.2.7"},
		securePort: "8443",
	})

	assertContactOrigins(
		t,
		selectContactOrigins(t, seed, true, answeredAt(t, "https://192.0.2.7:8443")),
		"https://192.0.2.7:8443",
		"https://example.org:443",
		"https://example.org:8443",
		"http://example.org:8090",
		"http://192.0.2.7:8090",
	)
	assertContactOrigins(
		t,
		selectContactOrigins(t, seed, true, answeredAt(t, "https://example.org:443")),
		"https://example.org:443",
		"https://example.org:8443",
		"http://example.org:8090",
		"https://192.0.2.7:8443",
		"http://192.0.2.7:8090",
	)
	assertContactOrigins(
		t,
		selectContactOrigins(t, seed, false, answeredAt(t, "http://192.0.2.7:8090")),
		"http://192.0.2.7:8090",
		"http://example.org:8090",
	)
}

func TestContactEndpointsNeverAddAnsweredEndpointOutsideAdvertisement(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{hosts: []string{"192.0.2.7"}})
	unadvertised := []string{
		"http://127.0.0.1:8090",
		"http://192.0.2.8:8090",
		"http://192.0.2.7:8091",
		"https://192.0.2.7:443",
	}
	for _, origin := range unadvertised {
		for _, preferHTTPS := range []bool{false, true} {
			assertContactOrigins(
				t,
				selectContactOrigins(t, seed, preferHTTPS, answeredAt(t, origin)),
				"http://192.0.2.7:8090",
			)
		}
	}
}

func TestContactEndpointsKeepRememberedHTTPBehindHTTPSPreference(t *testing.T) {
	seed := contactSeed(t, contactAdvertisement{
		hosts:      []string{"example.org"},
		securePort: "8443",
	})

	assertContactOrigins(
		t,
		selectContactOrigins(t, seed, true, answeredAt(t, "http://example.org:8090")),
		"https://example.org:443",
		"https://example.org:8443",
		"http://example.org:8090",
	)
	assertContactOrigins(
		t,
		selectContactOrigins(t, seed, false, answeredAt(t, "https://example.org:8443")),
		"http://example.org:8090",
	)
}

func TestContactEndpointsRefuseUnaddressablePeer(t *testing.T) {
	_, err := ContactEndpoints(
		yagomodel.Seed{Hash: yagomodel.Hash("ABCDEFGHIJKL")},
		contactPath,
		true,
		noAnsweredEndpoint(),
	)
	if !errors.Is(err, yagomodel.ErrBadSeed) {
		t.Fatalf("unaddressable peer error = %v", err)
	}
}
