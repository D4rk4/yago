package crawlbroker

import (
	"net/netip"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestCrawlerRuntimePolicyEndpointReadsRegistrySnapshot(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.CrawlDelay = 250 * time.Millisecond
	policy.UserAgent = "crawler-policy-test"
	server := newExchangeServer(nil, nil, crawlerControlDefaults{
		runtimePolicy: policy,
		storagePressurePolicy: yagocrawlcontract.StoragePressurePolicy{
			ReservedFreeBytes:       55,
			RecoveryHysteresisBytes: 7,
		},
	})
	read, err := server.ReadRuntimePolicy(t.Context(), &crawlrpc.CrawlerRuntimePolicyRequest{
		WorkerId: "worker-policy",
	})
	if err != nil {
		t.Fatalf("read runtime policy: %v", err)
	}
	decoded, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(read)
	if err != nil || !decoded.Equal(policy) {
		t.Fatalf("decoded policy = %+v/%v, want %+v", decoded, err, policy)
	}
	if read.StorageReservedFreeBytes == nil ||
		read.StoragePressureHysteresisBytes == nil ||
		read.GetStorageReservedFreeBytes() != 55 ||
		read.GetStoragePressureHysteresisBytes() != 7 {
		t.Fatalf("startup storage policy = %+v", read)
	}
	if _, err := server.ReadRuntimePolicy(
		t.Context(),
		&crawlrpc.CrawlerRuntimePolicyRequest{},
	); status.Code(
		err,
	) != codes.InvalidArgument {
		t.Fatalf("invalid worker policy read = %v", err)
	}
	server.control.mu.Lock()
	server.control.runtimePolicy.MaximumDepth = 0
	server.control.mu.Unlock()
	if _, err := server.ReadRuntimePolicy(t.Context(), &crawlrpc.CrawlerRuntimePolicyRequest{
		WorkerId: "worker-policy",
	}); status.Code(err) != codes.Internal {
		t.Fatalf("invalid stored policy read = %v", err)
	}
	if _, err := server.heartbeatResult(nil, nil, 0, nil); status.Code(err) != codes.Internal {
		t.Fatalf("invalid stored heartbeat policy = %v", err)
	}
}

func TestControlRegistryRuntimePolicyValidatesAndClones(t *testing.T) {
	registry := newControlRegistry()
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.AllowedPrivateCIDRs = []netip.Prefix{netip.MustParsePrefix("10.4.0.0/16")}
	if !registry.SetRuntimePolicy(policy) {
		t.Fatal("valid runtime policy rejected")
	}
	policy.AllowedPrivateCIDRs[0] = netip.MustParsePrefix("10.5.0.0/16")
	if got := registry.RuntimePolicy().AllowedPrivateCIDRs[0].String(); got != "10.4.0.0/16" {
		t.Fatalf("registry policy mutated through input slice: %s", got)
	}
	copy := registry.RuntimePolicy()
	copy.AllowedPrivateCIDRs[0] = netip.MustParsePrefix("10.6.0.0/16")
	if got := registry.RuntimePolicy().AllowedPrivateCIDRs[0].String(); got != "10.4.0.0/16" {
		t.Fatalf("registry policy mutated through output slice: %s", got)
	}
	if !registry.SetRuntimePolicy(registry.RuntimePolicy()) {
		t.Fatal("equal runtime policy rejected")
	}
	invalid := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	invalid.MaximumDepth = 0
	if registry.SetRuntimePolicy(invalid) {
		t.Fatal("invalid runtime policy accepted")
	}
}
