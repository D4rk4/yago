package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultUserAgentCarriesBuildVersion(t *testing.T) {
	if version == "" {
		t.Fatal("build version must not be empty")
	}
	versionPattern := regexp.MustCompile(
		`^(?:v[0-9]+\.[0-9]+\.[0-9]+|[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev)$`,
	)
	if !versionPattern.MatchString(version) {
		t.Fatalf("build version %q must use vN.N.N or YYYY.MM.DD-dev", version)
	}
	want := "yago-crawler/" + version + " (+https://github.com/D4rk4/yago/)"
	if DefaultUserAgent != want {
		t.Fatalf("default user agent = %q, want %q", DefaultUserAgent, want)
	}
	if !strings.Contains(DefaultUserAgent, version) {
		t.Fatalf("user agent %q does not carry the version", DefaultUserAgent)
	}
}

func TestCrawlerContainerRequiresCallerStampedBuildIdentity(t *testing.T) {
	contents, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		`ARG VERSION=`,
		`set -eu;`,
		`[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev`,
		`main.version=${VERSION}`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Dockerfile missing %q", required)
		}
	}
	if strings.Contains(text, `date -u`) {
		t.Fatal("Dockerfile derives a cacheable build date internally")
	}
}

func TestDefaultUserAgentReachesCrawlConfig(t *testing.T) {
	cfg, err := LoadServiceConfig(func(key string) string {
		if key == EnvNodeRPCAddr {
			return "node:9091"
		}

		return ""
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Crawl.UserAgent != DefaultUserAgent {
		t.Fatalf("crawl user agent = %q, want the branded default", cfg.Crawl.UserAgent)
	}
}
