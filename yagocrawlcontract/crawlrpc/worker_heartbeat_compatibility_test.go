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
		WorkerId:      "current",
		ActiveFetches: &zero,
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
	if decoded.GetWorkerId() != "legacy" || decoded.ActiveFetches != nil {
		t.Fatalf("decoded legacy heartbeat = %+v", decoded)
	}
}

func TestLegacyWorkerHeartbeatDecoderIgnoresActiveFetches(t *testing.T) {
	descriptor := legacyWorkerHeartbeatDescriptor(t)
	active := uint32(2)
	wire, err := proto.Marshal(&crawlrpc.WorkerHeartbeat{
		WorkerId:      "current",
		ActiveFetches: &active,
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
