package crawlrpc_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestWorkerHeartbeatActiveFetchesPreservesExplicitZero(t *testing.T) {
	zero := uint32(0)
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{
		WorkerId:                 "current",
		ActiveFetches:            &zero,
		AcknowledgedDirectiveIds: []uint64{4, 9},
	})
	if err != nil {
		t.Fatalf("marshal current heartbeat: %v", err)
	}
	decoded := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal current heartbeat: %v", err)
	}
	if decoded.ActiveFetches == nil || decoded.GetActiveFetches() != 0 {
		t.Fatalf(
			"decoded active fetches = %v/%d, want present zero",
			decoded.ActiveFetches,
			decoded.GetActiveFetches(),
		)
	}
	if got := decoded.GetAcknowledgedDirectiveIds(); len(got) != 2 || got[0] != 4 || got[1] != 9 {
		t.Fatalf("decoded directive acknowledgments = %v, want [4 9]", got)
	}
}

func TestWorkerHeartbeatReadsLegacyWireAsUnknownActivity(t *testing.T) {
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{WorkerId: "legacy"})
	if err != nil {
		t.Fatalf("marshal legacy heartbeat: %v", err)
	}
	decoded := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal legacy heartbeat: %v", err)
	}
	if decoded.GetWorkerId() != "legacy" || decoded.ActiveFetches != nil ||
		decoded.ConfirmActiveLeaseDeliveries != nil {
		t.Fatalf("decoded legacy heartbeat = %+v", decoded)
	}
}

func TestWorkerHeartbeatDeliveryConfirmationPreservesPresence(t *testing.T) {
	for _, confirmation := range []bool{false, true} {
		wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{
			WorkerId:                     "current",
			ConfirmActiveLeaseDeliveries: &confirmation,
		})
		if err != nil {
			t.Fatalf("marshal delivery confirmation %v: %v", confirmation, err)
		}
		decoded := &crawlrpc.WorkerHeartbeat{}
		if err := proto.Unmarshal(wire, decoded); err != nil {
			t.Fatalf("unmarshal delivery confirmation %v: %v", confirmation, err)
		}
		if decoded.ConfirmActiveLeaseDeliveries == nil ||
			decoded.GetConfirmActiveLeaseDeliveries() != confirmation {
			t.Fatalf("decoded delivery confirmation = %+v", decoded)
		}
		legacy := dynamicpb.NewMessage(legacyWorkerHeartbeatDescriptor(t))
		if err := proto.Unmarshal(wire, legacy); err != nil {
			t.Fatalf("legacy decoder rejected delivery confirmation: %v", err)
		}
	}
}

func TestLegacyWorkerHeartbeatDecoderIgnoresActiveFetches(t *testing.T) {
	descriptor := legacyWorkerHeartbeatDescriptor(t)
	active := uint32(2)
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{
		WorkerId:                 "current",
		ActiveFetches:            &active,
		AcknowledgedDirectiveIds: []uint64{12},
	})
	if err != nil {
		t.Fatalf("marshal current heartbeat: %v", err)
	}
	legacy := dynamicpb.NewMessage(descriptor)
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy decoder rejected additive heartbeat: %v", err)
	}
	workerField := descriptor.Fields().ByNumber(1)
	if got := legacy.Get(workerField).String(); got != "current" {
		t.Fatalf("legacy worker id = %q, want current", got)
	}
}

func TestControlDirectiveIdentityRoundTrips(t *testing.T) {
	wire, err := proto.Marshal(&crawlrpc.CrawlControlDirective{
		DirectiveId:       73,
		Kind:              crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_ACTIVE_RUNS,
		MaximumActiveRuns: 37,
	})
	if err != nil {
		t.Fatalf("marshal directive: %v", err)
	}
	decoded := &crawlrpc.CrawlControlDirective{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal directive: %v", err)
	}
	if decoded.GetDirectiveId() != 73 ||
		decoded.GetKind() != crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_ACTIVE_RUNS ||
		decoded.GetMaximumActiveRuns() != 37 {
		t.Fatalf("directive = %+v, want id 73 and active runs 37", decoded)
	}
}

func TestProcessRateDirectiveIsAdditive(t *testing.T) {
	wire, err := proto.Marshal(&crawlrpc.CrawlControlDirective{
		Kind:                  crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_PROCESS_RATE,
		ProcessPagesPerSecond: 27,
	})
	if err != nil {
		t.Fatalf("marshal process rate: %v", err)
	}
	decoded := &crawlrpc.CrawlControlDirective{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal process rate: %v", err)
	}
	if decoded.GetKind() !=
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_PROCESS_RATE ||
		decoded.GetProcessPagesPerSecond() != 27 {
		t.Fatalf("process rate directive = %+v", decoded)
	}
	legacy := dynamicpb.NewMessage(emptyLegacyMessageDescriptor(t, "CrawlControlDirective"))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy directive rejected additive process rate: %v", err)
	}
}

func TestMaximumRedirectsDirectiveIsAdditive(t *testing.T) {
	wire, err := proto.Marshal(&crawlrpc.CrawlControlDirective{
		Kind:             crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_MAXIMUM_REDIRECTS,
		MaximumRedirects: 7,
	})
	if err != nil {
		t.Fatalf("marshal maximum redirects: %v", err)
	}
	decoded := &crawlrpc.CrawlControlDirective{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal maximum redirects: %v", err)
	}
	if decoded.GetKind() !=
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_MAXIMUM_REDIRECTS ||
		decoded.GetMaximumRedirects() != 7 {
		t.Fatalf("maximum redirects directive = %+v", decoded)
	}
	legacy := dynamicpb.NewMessage(emptyLegacyMessageDescriptor(t, "CrawlControlDirective"))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy directive rejected additive maximum redirects: %v", err)
	}
}

func TestWorkerHeartbeatStoragePressurePreservesExplicitZero(t *testing.T) {
	zero := uint64(0)
	available := false
	pressured := true
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{
		StorageAvailableBytes:       &zero,
		StorageMeasurementAvailable: &available,
		StoragePressure:             &pressured,
	})
	if err != nil {
		t.Fatalf("marshal storage heartbeat: %v", err)
	}
	decoded := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal storage heartbeat: %v", err)
	}
	if decoded.StorageAvailableBytes == nil ||
		decoded.StorageMeasurementAvailable == nil ||
		decoded.StoragePressure == nil || decoded.GetStorageAvailableBytes() != 0 ||
		decoded.GetStorageMeasurementAvailable() || !decoded.GetStoragePressure() {
		t.Fatalf("decoded storage heartbeat = %+v", decoded)
	}
	legacy := dynamicpb.NewMessage(legacyWorkerHeartbeatDescriptor(t))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy heartbeat rejected storage fields: %v", err)
	}
}

func TestWorkerHeartbeatResultStoragePolicyIsAdditiveAndOptional(t *testing.T) {
	zero := uint64(0)
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeatResult{
		StorageReservedFreeBytes:       &zero,
		StoragePressureHysteresisBytes: &zero,
	})
	if err != nil {
		t.Fatalf("marshal storage policy: %v", err)
	}
	decoded := &crawlrpc.WorkerHeartbeatResult{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal storage policy: %v", err)
	}
	if decoded.StorageReservedFreeBytes == nil ||
		decoded.StoragePressureHysteresisBytes == nil {
		t.Fatalf("explicit zero policy lost presence: %+v", decoded)
	}
	legacy := dynamicpb.NewMessage(emptyLegacyMessageDescriptor(t, "WorkerHeartbeatResult"))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy result rejected storage policy: %v", err)
	}
	legacyWire, err := proto.Marshal(dynamicpb.NewMessage(
		emptyLegacyMessageDescriptor(t, "WorkerHeartbeatResult"),
	))
	if err != nil {
		t.Fatalf("marshal legacy result: %v", err)
	}
	legacyDecoded := &crawlrpc.WorkerHeartbeatResult{}
	if err := proto.Unmarshal(legacyWire, legacyDecoded); err != nil {
		t.Fatalf("current result rejected legacy wire: %v", err)
	}
	if legacyDecoded.StorageReservedFreeBytes != nil ||
		legacyDecoded.StoragePressureHysteresisBytes != nil {
		t.Fatalf("legacy result invented policy presence: %+v", legacyDecoded)
	}
}

func TestWorkerHeartbeatURLDenylistFieldsAreAdditive(t *testing.T) {
	revision := make([]byte, 32)
	revision[0] = 7
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{
		WorkerId: "current", UrlDenylistRevision: revision, UrlDenylistBootstrap: true,
	})
	if err != nil {
		t.Fatalf("marshal URL denylist heartbeat: %v", err)
	}
	decoded := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal URL denylist heartbeat: %v", err)
	}
	if !decoded.GetUrlDenylistBootstrap() ||
		!proto.Equal(decoded, &crawlrpc.WorkerHeartbeat{
			WorkerId: "current", UrlDenylistRevision: revision, UrlDenylistBootstrap: true,
		}) {
		t.Fatalf("decoded URL denylist heartbeat = %+v", decoded)
	}
	legacy := dynamicpb.NewMessage(legacyWorkerHeartbeatDescriptor(t))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy heartbeat rejected URL denylist fields: %v", err)
	}
	resultWire, err := proto.Marshal(&crawlrpc.WorkerHeartbeatResult{
		UrlDenylist: &crawlrpc.CrawlURLDenylist{
			Revision: revision, ExactUrls: []string{"https://blocked.example/"},
			Domains: []string{"denied.example"},
		},
	})
	if err != nil {
		t.Fatalf("marshal URL denylist result: %v", err)
	}
	legacyResult := dynamicpb.NewMessage(emptyLegacyMessageDescriptor(t, "WorkerHeartbeatResult"))
	if err := proto.Unmarshal(resultWire, legacyResult); err != nil {
		t.Fatalf("legacy result rejected URL denylist policy: %v", err)
	}
}

func legacyWorkerHeartbeatDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("legacy_worker_heartbeat.proto"),
		Package: proto.String("compat"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("WorkerHeartbeat"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:   proto.String("worker_id"),
				Number: proto.Int32(1),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("legacy heartbeat descriptor: %v", err)
	}

	return file.Messages().ByName("WorkerHeartbeat")
}

func emptyLegacyMessageDescriptor(
	t *testing.T,
	name string,
) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("legacy_empty.proto"),
		Package:     proto.String("compat"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String(name)}},
	}, nil)
	if err != nil {
		t.Fatalf("legacy empty descriptor: %v", err)
	}

	return file.Messages().ByName(protoreflect.Name(name))
}
