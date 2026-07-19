package remotecrawl

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type observationRecorder struct {
	observations []Observation
}

func (r *observationRecorder) ObserveRemoteCrawl(observation Observation) {
	r.observations = append(r.observations, observation)
}

func TestRemoteCrawlConfigurationRejectsUnsafeBounds(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{
			name:   "negative request rate",
			change: func(config *Config) { config.RequestsPerMinute = -1 },
		},
		{
			name:   "excessive request rate",
			change: func(config *Config) { config.RequestsPerMinute = MaximumRequestsPerMinute + 1 },
		},
		{
			name:   "negative outstanding leases",
			change: func(config *Config) { config.OutstandingPerPeer = -1 },
		},
		{
			name:   "excessive outstanding leases",
			change: func(config *Config) { config.OutstandingPerPeer = MaximumOutstandingPerPeer + 1 },
		},
		{name: "short lease", change: func(config *Config) { config.LeaseTTL = time.Millisecond }},
		{
			name:   "long lease",
			change: func(config *Config) { config.LeaseTTL = MaximumLeaseTTL + time.Second },
		},
		{name: "negative capacity", change: func(config *Config) { config.QueueCapacity = -1 }},
		{
			name:   "excessive capacity",
			change: func(config *Config) { config.QueueCapacity = MaximumQueueCapacity + 1 },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := remoteConfig(time.Now)
			test.change(&config)
			if _, err := config.normalized(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestRemoteCrawlConfigurationAppliesDefaults(t *testing.T) {
	config, err := (Config{}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if config.RequestsPerMinute != DefaultRequestsPerMinute ||
		config.OutstandingPerPeer != DefaultOutstandingPerPeer ||
		config.LeaseTTL != DefaultLeaseTTL || config.QueueCapacity != DefaultQueueCapacity ||
		config.Now == nil {
		t.Fatalf("defaults = %+v", config)
	}
}

func TestDestinationCandidateRejectsInvalidURLs(t *testing.T) {
	for _, rawURL := range []string{
		"",
		strings.Repeat("x", MaximumReceiptURLBytes+1),
		"%",
		"relative/path",
		"mailto:user@example.com",
		"ftp://example.com/a",
		"https://user@example.com/a",
		"https://example.com:invalid/a",
		"https://example.com:444/a",
		"https://[fe80::1%25eth0]/a",
	} {
		if _, err := parseDestinationCandidate(rawURL); err == nil {
			t.Fatalf("candidate %q accepted", rawURL)
		}
	}
}

func TestAllowedDestinationNormalizationRejectsInvalidEntries(t *testing.T) {
	tooMany := make([]string, MaximumAllowedDestinations+1)
	for index := range tooMany {
		tooMany[index] = "example.com"
	}
	for _, entries := range [][]string{
		tooMany,
		{},
		{" "},
		{"0.0.0.0/0"},
		{"::/0"},
		{"not_a_domain"},
	} {
		if _, err := NormalizeAllowedDestinations(entries); err == nil {
			t.Fatalf("destinations %+v accepted", entries)
		}
	}
	normalized, err := NormalizeAllowedDestinations([]string{
		" Example.COM. ", "example.com", "192.0.2.17/24", "192.0.2.0/24",
	})
	if err != nil || strings.Join(normalized, ",") != "example.com,192.0.2.0/24" {
		t.Fatalf("normalized destinations = %v, %v", normalized, err)
	}
}

func TestDestinationPolicyCoversAddressAdmissionBoundaries(t *testing.T) {
	resolverFailure := errors.New("resolver failure")
	tests := []struct {
		name     string
		entries  []string
		resolver HostResolver
		rawURL   string
		want     string
	}{
		{
			name:     "resolver failure",
			entries:  []string{"example.com"},
			rawURL:   testURLA,
			resolver: func(context.Context, string) ([]netip.Addr, error) { return nil, resolverFailure },
		},
		{
			name: "empty resolution", entries: []string{"example.com"}, rawURL: testURLA,
			resolver: func(context.Context, string) ([]netip.Addr, error) { return nil, nil },
		},
		{
			name: "private domain resolution", entries: []string{"example.com"}, rawURL: testURLA,
			resolver: func(context.Context, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			},
		},
		{
			name: "unlisted domain", entries: []string{"other.example"}, rawURL: testURLA,
			resolver: publicResolver,
		},
		{
			name:     "unlisted literal",
			entries:  []string{"192.0.2.0/24"},
			rawURL:   "https://198.51.100.1/a",
			resolver: publicResolver,
		},
		{
			name: "unsafe literal", entries: []string{"127.0.0.0/8"}, rawURL: "http://127.0.0.1/a",
			resolver: publicResolver,
		},
		{
			name:     "invalid domain",
			entries:  []string{"example.com"},
			rawURL:   "https://bad..example/a",
			resolver: publicResolver,
		},
		{
			name:     "allowed literal",
			entries:  []string{"93.184.216.0/24"},
			rawURL:   "http://93.184.216.34/a",
			resolver: publicResolver,
			want:     "http://93.184.216.34/a",
		},
		{
			name:     "allowed ipv6 literal",
			entries:  []string{"2606:2800:220:1::/64"},
			rawURL:   "https://[2606:2800:220:1::1]/a",
			resolver: publicResolver,
			want:     "https://[2606:2800:220:1::1]/a",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := newDestinationPolicy(test.entries, test.resolver)
			if err != nil {
				t.Fatal(err)
			}
			got, err := policy.Admit(t.Context(), test.rawURL)
			if test.want == "" {
				if err == nil {
					t.Fatalf("Admit = %q", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("Admit = %q, %v", got, err)
			}
		})
	}
}

func TestDestinationPolicyUsesTheDefaultResolver(t *testing.T) {
	if _, err := newDestinationPolicy([]string{" "}, publicResolver); err == nil {
		t.Fatal("empty policy accepted")
	}
	defaultResolverPolicy, err := newDestinationPolicy([]string{"example.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := defaultResolverPolicy.Admit(canceled, testURLA); err == nil {
		t.Fatal("canceled default resolution succeeded")
	}
}

func TestDomainValidationBoundaries(t *testing.T) {
	invalid := []string{
		"", strings.Repeat("a", 254), "a..b", "-a.example", "a-.example",
		strings.Repeat("a", 64) + ".example", "bad_name.example",
	}
	for _, domain := range invalid {
		if validDomain(domain) {
			t.Fatalf("domain %q accepted", domain)
		}
	}
	if !validDomain("a-b.example") {
		t.Fatal("valid domain rejected")
	}
}

func TestRemoteCrawlValueBoundaries(t *testing.T) {
	if defaultDestinationPort("https") != 443 || defaultDestinationPort("http") != 80 {
		t.Fatal("default destination ports changed")
	}
	if err := validateDestinationPort(
		&url.URL{Scheme: "https", Host: "example.com:0"},
	); err == nil {
		t.Fatal("zero destination port accepted")
	}
	if canonicalURLHost("2001:db8::1") != "[2001:db8::1]" ||
		canonicalURLHost("example.com") != "example.com" {
		t.Fatal("canonical host formatting changed")
	}
	if publicationUnixNano(time.Time{}) != 0 || publicationTime(0) != (time.Time{}) {
		t.Fatal("zero publication changed")
	}
	publication := time.Unix(100, 7)
	if publicationTime(publicationUnixNano(publication)) != publication.UTC() {
		t.Fatal("publication round trip changed")
	}
	if boundedString("  abc  ", 3) != "abc" || boundedString("abcdef", 3) != "abc" {
		t.Fatal("bounded string changed")
	}
	if availableLeaseSlots(0, 0) != 0 || availableLeaseSlots(10, MaximumOutstandingPerPeer) != 0 ||
		availableLeaseSlots(2, 2) != 0 || availableLeaseSlots(2, 1) != 1 {
		t.Fatal("available lease slots changed")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if !leaseSelectionStopped(canceled) || leaseSelectionStopped(t.Context()) {
		t.Fatal("lease selection cancellation changed")
	}
	deadline, cancelDeadline := minimumDeadline(t.Context(), 0)
	defer cancelDeadline()
	if _, present := deadline.Deadline(); !present {
		t.Fatal("minimum deadline missing")
	}
	if _, err := yagomodel.ParseHash(testPeerA.String()); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCrawlCodecsRejectMalformedValues(t *testing.T) {
	if _, err := (jsonCodec[chan int]{}).Encode(make(chan int)); err == nil {
		t.Fatal("unsupported JSON value encoded")
	}
	if _, err := (jsonCodec[queueRecord]{}).Decode([]byte("{")); err == nil {
		t.Fatal("malformed JSON decoded")
	}
	if _, err := (uint64Codec{}).Decode([]byte{1}); err == nil {
		t.Fatal("short sequence decoded")
	}
	encoded, err := (uint64Codec{}).Encode(17)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (uint64Codec{}).Decode(encoded)
	if err != nil || decoded != 17 {
		t.Fatalf("sequence round trip = %d, %v", decoded, err)
	}
	if _, valid := leaseExpiry([]byte("short")); valid {
		t.Fatal("short lease expiry accepted")
	}
}

func TestRemoteCrawlObservationNotifiesConfiguredObservers(t *testing.T) {
	recorder := &observationRecorder{}
	broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	broker.observers = []Observer{nil, recorder}
	observation := Observation{
		Action: "lease", Outcome: "accepted", Peer: testPeerA,
		URLHash: testPeerB, Count: 1,
	}
	broker.observe(t.Context(), observation, true)
	broker.observe(t.Context(), Observation{Action: "stage", Outcome: "accepted"}, false)
	if len(recorder.observations) != 2 || recorder.observations[0] != observation {
		t.Fatalf("observations = %+v", recorder.observations)
	}
}
