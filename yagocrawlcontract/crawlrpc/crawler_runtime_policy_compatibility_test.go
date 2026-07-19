package crawlrpc_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestCrawlerRuntimePolicyBrowserSandboxIsAdditiveAndPresent(t *testing.T) {
	descriptor := crawlrpc.File_crawlexchange_proto.Messages().ByName(
		"CrawlerRuntimePolicy",
	)
	field := descriptor.Fields().ByName("browser_sandbox")
	if field == nil || field.Number() != 15 || !field.HasPresence() {
		t.Fatalf("browser sandbox descriptor = %+v, want optional field 15", field)
	}
	enabled := true
	wire, err := proto.Marshal(&crawlrpc.CrawlerRuntimePolicy{BrowserSandbox: &enabled})
	if err != nil {
		t.Fatalf("marshal current policy: %v", err)
	}
	legacy := emptyLegacyMessageDescriptor(t, "CrawlerRuntimePolicy")
	if err := proto.Unmarshal(wire, dynamicpb.NewMessage(legacy)); err != nil {
		t.Fatalf("legacy decoder rejected browser sandbox field: %v", err)
	}
	decoded := &crawlrpc.CrawlerRuntimePolicy{}
	if err := proto.Unmarshal(nil, decoded); err != nil {
		t.Fatalf("decode legacy policy: %v", err)
	}
	if decoded.BrowserSandbox != nil {
		t.Fatalf("legacy policy invented browser sandbox presence: %+v", decoded)
	}
}

func TestCrawlerRuntimePolicyProcessFacilitiesAreAdditiveAndPresent(t *testing.T) {
	descriptor := crawlrpc.File_crawlexchange_proto.Messages().ByName(
		"CrawlerRuntimePolicy",
	)
	browserPath := descriptor.Fields().ByName("browser_path")
	metricsAddress := descriptor.Fields().ByName("metrics_address")
	if browserPath == nil || browserPath.Number() != 16 || !browserPath.HasPresence() {
		t.Fatalf("browser path descriptor = %+v, want optional field 16", browserPath)
	}
	if metricsAddress == nil || metricsAddress.Number() != 17 || !metricsAddress.HasPresence() {
		t.Fatalf("metrics address descriptor = %+v, want optional field 17", metricsAddress)
	}
	path := "/usr/bin/firefox-esr"
	address := "127.0.0.1:9101"
	wire, err := proto.Marshal(&crawlrpc.CrawlerRuntimePolicy{
		BrowserPath: &path, MetricsAddress: &address,
	})
	if err != nil {
		t.Fatalf("marshal current policy: %v", err)
	}
	legacy := emptyLegacyMessageDescriptor(t, "CrawlerRuntimePolicy")
	if err := proto.Unmarshal(wire, dynamicpb.NewMessage(legacy)); err != nil {
		t.Fatalf("legacy decoder rejected process facility fields: %v", err)
	}
	decoded := &crawlrpc.CrawlerRuntimePolicy{}
	if err := proto.Unmarshal(nil, decoded); err != nil {
		t.Fatalf("decode legacy policy: %v", err)
	}
	if decoded.BrowserPath != nil || decoded.MetricsAddress != nil {
		t.Fatalf("legacy policy invented process facility presence: %+v", decoded)
	}
}
