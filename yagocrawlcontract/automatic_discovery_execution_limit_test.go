package yagocrawlcontract_test

import (
	"math"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func automaticDiscoveryLimitPolicy() yagocrawlcontract.CrawlerRuntimePolicy {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.AutomaticDiscoveryLimits = []yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		{
			ProfileName:         "swarm-seed",
			MaximumDepth:        1,
			MaximumPagesPerHost: 25,
			MaximumPagesPerRun:  25,
		},
		{
			ProfileName:         "web-fallback-seed",
			MaximumDepth:        3,
			MaximumPagesPerHost: 50,
			MaximumPagesPerRun:  40,
		},
	}

	return policy
}

func TestAutomaticDiscoveryExecutionLimitsRoundTrip(t *testing.T) {
	policy := automaticDiscoveryLimitPolicy()
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	decoded, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(message)
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if !decoded.Equal(policy) {
		t.Fatalf("decoded policy = %+v, want %+v", decoded, policy)
	}
	decoded.AutomaticDiscoveryLimits[0].MaximumPagesPerRun = 24
	if decoded.Equal(policy) {
		t.Fatal("different automatic discovery limits compared equal")
	}
}

func TestLegacyRuntimePolicyKeepsAutomaticDiscoveryFallback(t *testing.T) {
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(
		yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	fallback := automaticDiscoveryLimitPolicy()
	decoded, err := yagocrawlcontract.CrawlerRuntimePolicyFromProtoWithFallback(
		message,
		fallback,
	)
	if err != nil {
		t.Fatalf("decode legacy policy: %v", err)
	}
	if !decoded.Equal(fallback) {
		t.Fatalf("legacy policy = %+v, want fallback %+v", decoded, fallback)
	}
	decoded.AutomaticDiscoveryLimits[0].MaximumPagesPerRun = 1
	if fallback.AutomaticDiscoveryLimits[0].MaximumPagesPerRun != 25 {
		t.Fatal("decoded fallback shares automatic discovery limit storage")
	}
}

func TestAutomaticDiscoveryExecutionLimitsRejectInvalidValues(t *testing.T) {
	tooMany := make(
		[]yagocrawlcontract.AutomaticDiscoveryExecutionLimit,
		yagocrawlcontract.MaximumAutomaticDiscoveryExecutionLimits+1,
	)
	for index := range tooMany {
		tooMany[index] = yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
			ProfileName:         strings.Repeat("x", index+1),
			MaximumPagesPerHost: 1,
			MaximumPagesPerRun:  1,
		}
	}
	cases := [][]yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		tooMany,
		{{ProfileName: "", MaximumPagesPerHost: 1, MaximumPagesPerRun: 1}},
		{{ProfileName: "bad\nname", MaximumPagesPerHost: 1, MaximumPagesPerRun: 1}},
		{{
			ProfileName: strings.Repeat(
				"x",
				yagocrawlcontract.MaximumAutomaticDiscoveryProfileNameBytes+1,
			),
			MaximumPagesPerHost: 1,
			MaximumPagesPerRun:  1,
		}},
		{
			{ProfileName: "duplicate", MaximumPagesPerHost: 1, MaximumPagesPerRun: 1},
			{ProfileName: "duplicate", MaximumPagesPerHost: 1, MaximumPagesPerRun: 1},
		},
		{
			{
				ProfileName:         "negative-depth",
				MaximumDepth:        -1,
				MaximumPagesPerHost: 1,
				MaximumPagesPerRun:  1,
			},
		},
		{
			{
				ProfileName:         "deep",
				MaximumDepth:        yagocrawlcontract.MaxCrawlDepth + 1,
				MaximumPagesPerHost: 1,
				MaximumPagesPerRun:  1,
			},
		},
		{{ProfileName: "host", MaximumPagesPerHost: 0, MaximumPagesPerRun: 1}},
		{{ProfileName: "run", MaximumPagesPerHost: 1, MaximumPagesPerRun: 0}},
	}
	for index, limits := range cases {
		policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
		policy.AutomaticDiscoveryLimits = limits
		if err := policy.Validate(); err == nil {
			t.Errorf("case %d accepted invalid limits", index)
		}
	}
}

func TestAutomaticDiscoveryExecutionLimitProtoRejectsMissingAndOverflowingValues(t *testing.T) {
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(
		automaticDiscoveryLimitPolicy(),
	)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	message.AutomaticDiscoveryLimits[0] = nil
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(message); err == nil {
		t.Fatal("missing automatic discovery limit decoded")
	}

	message, err = yagocrawlcontract.CrawlerRuntimePolicyToProto(
		automaticDiscoveryLimitPolicy(),
	)
	if err != nil {
		t.Fatalf("encode host overflow policy: %v", err)
	}
	message.AutomaticDiscoveryLimits[0].MaximumPagesPerHost = math.MaxUint64
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(message); err == nil {
		t.Fatal("overflowing automatic discovery host limit decoded")
	}

	message, err = yagocrawlcontract.CrawlerRuntimePolicyToProto(
		automaticDiscoveryLimitPolicy(),
	)
	if err != nil {
		t.Fatalf("encode run overflow policy: %v", err)
	}
	message.AutomaticDiscoveryLimits[0] = &crawlrpc.AutomaticDiscoveryExecutionLimit{
		ProfileName:         "overflow",
		MaximumPagesPerHost: 1,
		MaximumPagesPerRun:  math.MaxUint64,
	}
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(message); err == nil {
		t.Fatal("overflowing automatic discovery run limit decoded")
	}
}
