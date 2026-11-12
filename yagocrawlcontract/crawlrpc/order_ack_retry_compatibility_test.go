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

func TestOrderAckRetryDeadlineIsAdditive(t *testing.T) {
	descriptor := (&crawlrpc.OrderAck{}).ProtoReflect().Descriptor()
	field := descriptor.Fields().ByName("retry_after_nanoseconds")
	if field == nil || field.Number() != 11 || field.Kind() != protoreflect.Int64Kind {
		t.Fatalf("retry deadline descriptor = %v", field)
	}
	wire, err := proto.Marshal(&crawlrpc.OrderAck{
		LeaseId: "lease", Requeue: true, RetryAfterNanoseconds: 9_000_000_000,
	})
	if err != nil {
		t.Fatalf("marshal retry acknowledgment: %v", err)
	}
	decoded := &crawlrpc.OrderAck{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal retry acknowledgment: %v", err)
	}
	if decoded.GetRetryAfterNanoseconds() != 9_000_000_000 {
		t.Fatalf("retry deadline = %d", decoded.GetRetryAfterNanoseconds())
	}
	legacy := dynamicpb.NewMessage(legacyOrderAckDescriptor(t))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy decoder rejected retry acknowledgment: %v", err)
	}
	if got := legacy.Get(legacy.Descriptor().Fields().ByNumber(1)).String(); got != "lease" {
		t.Fatalf("legacy lease id = %q", got)
	}
}

func legacyOrderAckDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("legacy_order_ack.proto"),
		Package: proto.String("compat"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("OrderAck"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name: proto.String("lease_id"), Number: proto.Int32(1),
				Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build legacy descriptor: %v", err)
	}

	return file.Messages().ByName("OrderAck")
}
