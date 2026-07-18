package crawlrpc_test

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestLegacyCrawlOrderDecoderIgnoresSessionManifest(t *testing.T) {
	descriptor := legacyCrawlOrderMessageDescriptor(t)
	wire, err := proto.Marshal(&crawlrpc.CrawlOrderMessage{
		OrderJson:         []byte("order"),
		LeaseId:           "lease-a",
		Recovered:         true,
		RecoveredBatchEnd: true,
		RecoveredLeaseIds: []string{"lease-a"},
		RecoveredSessionLeaseIds: []string{
			"lease-a",
			"lease-b",
		},
	})
	if err != nil {
		t.Fatalf("marshal recovered crawl order: %v", err)
	}
	legacy := dynamicpb.NewMessage(descriptor)
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy decoder rejected recovery framing: %v", err)
	}
	legacyOrder := legacy.Get(descriptor.Fields().ByNumber(1)).Bytes()
	if !bytes.Equal(legacyOrder, []byte("order")) {
		t.Fatalf("legacy order payload = %q", legacyOrder)
	}
	if got := legacy.Get(descriptor.Fields().ByNumber(2)).String(); got != "lease-a" {
		t.Fatalf("legacy lease id = %q", got)
	}
	if !legacy.Get(descriptor.Fields().ByNumber(3)).Bool() ||
		!legacy.Get(descriptor.Fields().ByNumber(4)).Bool() {
		t.Fatalf("legacy recovery flags = %+v", legacy)
	}
	legacyBatch := legacy.Get(descriptor.Fields().ByNumber(5)).List()
	if legacyBatch.Len() != 1 || legacyBatch.Get(0).String() != "lease-a" {
		t.Fatalf("legacy recovery batch = %v", legacyBatch)
	}
}

func TestCurrentCrawlOrderDecoderTreatsLegacyWireAsOrdinary(t *testing.T) {
	descriptor := legacyCrawlOrderMessageDescriptor(t)
	legacy := dynamicpb.NewMessage(descriptor)
	legacy.Set(descriptor.Fields().ByNumber(1), protoreflect.ValueOfBytes([]byte("order")))
	legacy.Set(descriptor.Fields().ByNumber(2), protoreflect.ValueOfString("lease-a"))
	wire, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy crawl order: %v", err)
	}
	current := &crawlrpc.CrawlOrderMessage{}
	if err := proto.Unmarshal(wire, current); err != nil {
		t.Fatalf("current decoder rejected legacy crawl order: %v", err)
	}
	if !bytes.Equal(current.GetOrderJson(), []byte("order")) ||
		current.GetLeaseId() != "lease-a" || current.GetRecovered() ||
		current.GetRecoveredBatchEnd() || len(current.GetRecoveredLeaseIds()) != 0 ||
		len(current.GetRecoveredSessionLeaseIds()) != 0 {
		t.Fatalf("current legacy crawl order = %+v", current)
	}
}

func legacyCrawlOrderMessageDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("legacy_crawl_order.proto"),
		Package: proto.String("compat"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("CrawlOrderMessage"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("order_json"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				},
				{
					Name:   proto.String("lease_id"),
					Number: proto.Int32(2),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
				{
					Name:   proto.String("recovered"),
					Number: proto.Int32(3),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				},
				{
					Name:   proto.String("recovered_batch_end"),
					Number: proto.Int32(4),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				},
				{
					Name:   proto.String("recovered_lease_ids"),
					Number: proto.Int32(5),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("legacy crawl order descriptor: %v", err)
	}

	return file.Messages().ByName("CrawlOrderMessage")
}
