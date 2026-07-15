package searchcore

import (
	"strconv"
	"strings"
	"testing"
)

func TestResultSiteIdentity(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "apex", host: "example.com", want: "example.com"},
		{name: "www", host: "www.example.com", want: "example.com"},
		{name: "sibling", host: "docs.example.com", want: "example.com"},
		{name: "multi label suffix", host: "manuals.example.co.uk", want: "example.co.uk"},
		{name: "different site", host: "www.example.net", want: "example.net"},
		{name: "IPv4", host: "192.0.2.1", want: "192.0.2.1"},
		{name: "IPv6", host: "2001:DB8::1", want: "2001:db8::1"},
		{name: "localhost", host: "LOCALHOST", want: "localhost"},
		{name: "public suffix", host: "co.uk", want: "co.uk"},
		{name: "invalid", host: "Invalid Host", want: "invalid host"},
		{name: "trailing dot", host: "WWW.Example.COM.", want: "example.com"},
		{name: "empty", host: "  ", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resultSiteIdentity(test.host); got != test.want {
				t.Fatalf("resultSiteIdentity(%q) = %q, want %q", test.host, got, test.want)
			}
		})
	}
}

func TestResultHostCanHaveRegistrableParent(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{name: "domain", host: "docs.example.com", want: true},
		{name: "international domain", host: "docs.пример.рф", want: true},
		{name: "empty", host: "", want: false},
		{name: "leading empty label", host: ".example.com", want: false},
		{name: "interior empty label", host: "docs..example.com", want: false},
		{name: "address punctuation", host: "[2001:db8::1]", want: false},
		{name: "numeric address", host: "192.0.2.1", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resultHostCanHaveRegistrableParent(test.host); got != test.want {
				t.Fatalf(
					"resultHostCanHaveRegistrableParent(%q) = %t, want %t",
					test.host,
					got,
					test.want,
				)
			}
		})
	}
}

func BenchmarkStableResultDeferral(b *testing.B) {
	for _, size := range []int{10, 50} {
		results := realisticCrowdingWindow(size)
		b.Run(strconv.Itoa(size)+"/exact-host", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if got := benchmarkExactHostDeferral(results); len(got) != len(results) {
					b.Fatalf("result length = %d, want %d", len(got), len(results))
				}
			}
		})
		b.Run(strconv.Itoa(size)+"/registrable-site", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if got := deferCrowdedSites(results); len(got) != len(results) {
					b.Fatalf("result length = %d, want %d", len(got), len(results))
				}
			}
		})
	}
}

func realisticCrowdingWindow(size int) []Result {
	domains := []string{
		"alpha.com",
		"beta.net",
		"gamma.org",
		"delta.co.uk",
		"epsilon.io",
		"zeta.dev",
		"eta.app",
		"theta.info",
		"iota.biz",
		"kappa.edu",
	}
	prefixes := []string{"", "www.", "docs.", "forum.", "shop."}
	results := make([]Result, size)
	for ordinal := range results {
		results[ordinal].Host = prefixes[ordinal/len(domains)%len(prefixes)] + domains[ordinal%len(domains)]
	}

	return results
}

func benchmarkExactHostDeferral(results []Result) []Result {
	hostAppearances := make(map[string]int, len(results))
	head := make([]Result, 0, len(results))
	var overflow []Result
	for _, result := range results {
		host := strings.ToLower(result.Host)
		if host != "" && hostAppearances[host] >= siteCrowdingLimit {
			overflow = append(overflow, result)

			continue
		}
		hostAppearances[host]++
		head = append(head, result)
	}

	return append(head, overflow...)
}
