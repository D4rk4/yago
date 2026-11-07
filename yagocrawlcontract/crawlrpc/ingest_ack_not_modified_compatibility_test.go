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

func TestLegacyCrawlerDecoderIgnoresNotModifiedRecorded(t *testing.T) {
	descriptor := legacyIngestAckDescriptor(t)
	wire, err := proto.Marshal(&crawlrpc.IngestAck{
		Rejected:            true,
		RejectionRule:       "thin-content",
		NotModifiedRecorded: true,
	})
	if err != nil {
		t.Fatalf("marshal ingest acknowledgement: %v", err)
	}
	legacy := dynamicpb.NewMessage(descriptor)
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy decoder refused acknowledgement: %v", err)
	}
	if !legacy.Get(descriptor.Fields().ByNumber(1)).Bool() ||
		legacy.Get(descriptor.Fields().ByNumber(2)).String() != "thin-content" {
		t.Fatalf("legacy acknowledgement = %v", legacy)
	}
}

func TestLegacyNodeAcknowledgementLeavesNotModifiedUnrecorded(t *testing.T) {
	descriptor := legacyIngestAckDescriptor(t)
	legacy := dynamicpb.NewMessage(descriptor)
	legacy.Set(descriptor.Fields().ByNumber(1), protoreflect.ValueOfBool(false))
	legacy.Set(descriptor.Fields().ByNumber(2), protoreflect.ValueOfString(""))
	wire, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy acknowledgement: %v", err)
	}
	var current crawlrpc.IngestAck
	if err := proto.Unmarshal(wire, &current); err != nil {
		t.Fatalf("current decoder refused legacy acknowledgement: %v", err)
	}
	if current.GetNotModifiedRecorded() {
		t.Fatal("legacy acknowledgement reported a recorded not-modified observation")
	}
	recorded, err := proto.Marshal(&crawlrpc.IngestAck{NotModifiedRecorded: true})
	if err != nil {
		t.Fatalf("marshal recorded acknowledgement: %v", err)
	}
	var decoded crawlrpc.IngestAck
	if err := proto.Unmarshal(recorded, &decoded); err != nil {
		t.Fatalf("decode recorded acknowledgement: %v", err)
	}
	if !decoded.GetNotModifiedRecorded() {
		t.Fatal("recorded acknowledgement lost not_modified_recorded")
	}
}

func legacyIngestAckDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("legacy_ingest_ack.proto"),
		Package: proto.String("compat"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("IngestAck"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("rejected"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				},
				{
					Name:   proto.String("rejection_rule"),
					Number: proto.Int32(2),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build legacy ingest acknowledgement descriptor: %v", err)
	}

	return file.Messages().ByName("IngestAck")
}
