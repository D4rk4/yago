package yagomodel

import (
	"slices"
	"testing"
)

func TestSeedAdvertisedHostsPreservesOrderDeduplicatesAndCaps(t *testing.T) {
	primary, err := ParseHost("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	alternatives, err := ParseIP6(
		"192.0.2.1|2001:db8::1|2001:db8::1|2001:db8::2|2001:db8::3|2001:db8::4|2001:db8::5",
	)
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{IP: Some(primary), IP6: Some(alternatives)}

	hosts := seed.AdvertisedHosts()
	got := make([]string, 0, len(hosts))
	for _, host := range hosts {
		got = append(got, host.String())
	}
	want := []string{"192.0.2.1", "2001:db8::1", "2001:db8::2", "2001:db8::3", "2001:db8::4"}
	if !slices.Equal(got, want) {
		t.Fatalf("AdvertisedHosts = %v, want %v", got, want)
	}
}

func TestSeedAdvertisedHostsDoesNotResolveAliases(t *testing.T) {
	hostname, err := ParseHost("localhost")
	if err != nil {
		t.Fatal(err)
	}
	address, err := ParseHost("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{IP: Some(hostname), IP6: Some([]Host{address})}

	hosts := seed.AdvertisedHosts()
	if len(hosts) != 2 || hosts[0].String() != "localhost" || hosts[1].String() != "127.0.0.1" {
		t.Fatalf("AdvertisedHosts = %v, want distinct advertised aliases", hosts)
	}
	if (Seed{IP: Some(hostname)}).SharesAddress(Seed{IP: Some(address)}) {
		t.Fatal("hostname and numeric address were merged without seed-hash evidence")
	}
}

func TestSeedAdvertisedHostsCanonicalizesMappedIPv4AndHostnameCase(t *testing.T) {
	primary, err := ParseHost("::ffff:192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	alternatives, err := ParseIP6("192.0.2.1|2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	hostname, err := ParseHost("PEER.Example")
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{IP: Some(primary), IP6: Some(alternatives)}

	hosts := seed.AdvertisedHosts()
	if len(hosts) != 2 || hosts[0].String() != "192.0.2.1" ||
		hosts[1].String() != "2001:db8::1" {
		t.Fatalf("canonical address hosts = %v", hosts)
	}
	if got := (Seed{IP: Some(hostname)}).AdvertisedHosts(); len(got) != 1 ||
		got[0].String() != "peer.example" {
		t.Fatalf("canonical hostname = %v", got)
	}
}

func TestSeedIPv6OnlyNetworkAddressAndEndpoints(t *testing.T) {
	hosts, err := ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{IP6: Some(hosts), Port: Some(Port(8090))}

	address, ok := seed.NetworkAddress()
	if !ok || address != "[2001:db8::7]:8090" {
		t.Fatalf("NetworkAddress = %q, %v", address, ok)
	}
	endpoint, err := seed.HTTPEndpoint("/yacy/hello.html")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.String() != "http://[2001:db8::7]:8090/yacy/hello.html" {
		t.Fatalf("HTTPEndpoint = %q", endpoint)
	}
	endpoints, err := seed.ProtocolEndpoints("/yacy/query.html", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].String() != "http://[2001:db8::7]:8090/yacy/query.html" {
		t.Fatalf("ProtocolEndpoints = %v", endpoints)
	}
}

func TestSeedProtocolEndpointsFollowAddressThenSchemeOrder(t *testing.T) {
	seed := sslSeed(t)
	alternatives, err := ParseIP6("2001:db8::7|198.51.100.9")
	if err != nil {
		t.Fatal(err)
	}
	seed.IP6 = Some(alternatives)

	endpoints, err := seed.ProtocolEndpoints("/yacy/hello.html", true)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		got = append(got, endpoint.String())
	}
	want := []string{
		"https://192.0.2.7:8443/yacy/hello.html",
		"http://192.0.2.7:8090/yacy/hello.html",
		"https://[2001:db8::7]:8443/yacy/hello.html",
		"http://[2001:db8::7]:8090/yacy/hello.html",
		"https://198.51.100.9:8443/yacy/hello.html",
		"http://198.51.100.9:8090/yacy/hello.html",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ProtocolEndpoints = %v, want %v", got, want)
	}
}

func TestSeedWithPrimaryHostPromotesVerifiedAddress(t *testing.T) {
	primary, err := ParseHost("192.0.2.7")
	if err != nil {
		t.Fatal(err)
	}
	alternatives, err := ParseIP6("2001:db8::7|198.51.100.9")
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{IP: Some(primary), IP6: Some(alternatives), Port: Some(Port(8090))}

	promoted := seed.WithPrimaryHost(alternatives[0])
	address, ok := promoted.NetworkAddress()
	if !ok || address != "[2001:db8::7]:8090" {
		t.Fatalf("promoted NetworkAddress = %q, %v", address, ok)
	}
	hosts := promoted.AdvertisedHosts()
	got := make([]string, 0, len(hosts))
	for _, host := range hosts {
		got = append(got, host.String())
	}
	want := []string{"2001:db8::7", "192.0.2.7", "198.51.100.9"}
	if !slices.Equal(got, want) {
		t.Fatalf("promoted hosts = %v, want %v", got, want)
	}
}

func TestSeedWithPrimaryHostDoesNotMoveHostnameIntoIP6(t *testing.T) {
	hostname, err := ParseHost("peer.example")
	if err != nil {
		t.Fatal(err)
	}
	alternatives, err := ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{IP: Some(hostname), IP6: Some(alternatives)}

	promoted := seed.WithPrimaryHost(alternatives[0])
	hosts := promoted.AdvertisedHosts()
	if len(hosts) != 1 || hosts[0].String() != "2001:db8::7" {
		t.Fatalf("promoted hosts = %v, want only wire-valid numeric hosts", hosts)
	}
}
