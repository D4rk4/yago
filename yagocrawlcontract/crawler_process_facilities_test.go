package yagocrawlcontract_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerProcessFacilitiesCanonicalizeValidValues(t *testing.T) {
	browserPath, err := yagocrawlcontract.ParseCrawlerBrowserPath("  /usr/bin/firefox-esr  ")
	if err != nil || browserPath != "/usr/bin/firefox-esr" {
		t.Fatalf("browser path = %q, err = %v", browserPath, err)
	}
	metricsAddress, err := yagocrawlcontract.ParseCrawlerMetricsAddress(" [::1]:09101 ")
	if err != nil || metricsAddress != "[::1]:9101" {
		t.Fatalf("metrics address = %q, err = %v", metricsAddress, err)
	}
	for _, parse := range []func(string) (string, error){
		yagocrawlcontract.ParseCrawlerBrowserPath,
		yagocrawlcontract.ParseCrawlerMetricsAddress,
	} {
		value, parseErr := parse("  ")
		if parseErr != nil || value != "" {
			t.Fatalf("empty facility = %q, err = %v", value, parseErr)
		}
	}
	metricsAddress, err = yagocrawlcontract.ParseCrawlerMetricsAddress("127.0.0.1:9101")
	if err != nil || metricsAddress != "127.0.0.1:9101" {
		t.Fatalf("IPv4 metrics address = %q, err = %v", metricsAddress, err)
	}
}

func TestCrawlerBrowserPathRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{
		"firefox-esr",
		"/",
		"/usr/bin/chromium",
		"/usr/bin/firefox-bin",
		"/usr/bin/../bin/firefox-esr",
		"/usr/bin/firefox\n-esr",
		"/usr/bin/firefox\u200b-esr",
		"/usr/bin/firefox\u2028esr",
		"/usr/bin/firefox\u2029esr",
		string([]byte{'/', 0xff}),
		"/" + strings.Repeat("x", yagocrawlcontract.MaximumCrawlerBrowserPathBytes),
	} {
		if _, err := yagocrawlcontract.ParseCrawlerBrowserPath(value); err == nil {
			t.Errorf("browser path %q accepted", value)
		}
	}
}

func TestCrawlerMetricsAddressRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{
		"localhost:9101",
		":9101",
		"0.0.0.0:9101",
		"[::]:9101",
		"192.0.2.1:9101",
		"127.0.0.1",
		"127.0.0.1:0",
		"127.0.0.1:65536",
		"127.0.0.1:port",
		"127.0.0.1:9101\n",
		"127.0.0.1:9101\u2028",
		string([]byte{'1', '2', '7', '.', '0', '.', '0', '.', '1', ':', 0xff}),
		strings.Repeat("1", yagocrawlcontract.MaximumCrawlerMetricsAddressBytes+1),
	} {
		if _, err := yagocrawlcontract.ParseCrawlerMetricsAddress(value); err == nil {
			t.Errorf("metrics address %q accepted", value)
		}
	}
}
