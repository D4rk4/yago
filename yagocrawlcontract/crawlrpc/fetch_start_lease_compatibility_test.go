package crawlrpc_test

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestWorkerRegistrationFetchStartLeaseCapabilityIsAdditive(t *testing.T) {
	legacyDescriptor := legacyWorkerRegistrationDescriptor(t)
	legacy := dynamicpb.NewMessage(legacyDescriptor)
	legacy.Set(legacyDescriptor.Fields().ByNumber(1), protoreflect.ValueOfString("legacy"))
	legacy.Set(legacyDescriptor.Fields().ByNumber(2), protoreflect.ValueOfString("session"))
	legacyWire, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy registration: %v", err)
	}
	current := &crawlrpc.WorkerRegistration{}
	if err := proto.Unmarshal(legacyWire, current); err != nil {
		t.Fatalf("current decoder rejected legacy registration: %v", err)
	}
	if current.GetWorkerId() != "legacy" || current.GetWorkerSessionId() != "session" ||
		current.GetFetchStartLeases() {
		t.Fatalf("decoded legacy registration = %+v", current)
	}
	currentWire, err := proto.Marshal(&crawlrpc.WorkerRegistration{
		WorkerId: "current", WorkerSessionId: "session", FetchStartLeases: true,
	})
	if err != nil {
		t.Fatalf("marshal current registration: %v", err)
	}
	legacy = dynamicpb.NewMessage(legacyDescriptor)
	if err := proto.Unmarshal(currentWire, legacy); err != nil {
		t.Fatalf("legacy decoder rejected additive registration: %v", err)
	}
	if got := legacy.Get(legacyDescriptor.Fields().ByNumber(1)).String(); got != "current" {
		t.Fatalf("legacy worker id = %q, want current", got)
	}
}

func TestFetchStartLeaseRequestWireRoundTripAndLegacyDefaults(t *testing.T) {
	completed := uint64(0)
	want := &crawlrpc.FetchStartLeaseRequest{
		WorkerId: "worker", WorkerSessionId: "session", Sequence: 7,
		MaximumPermits: 256, CompletedSequence: &completed,
	}
	wire, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal lease request: %v", err)
	}
	got := &crawlrpc.FetchStartLeaseRequest{}
	if err := proto.Unmarshal(wire, got); err != nil {
		t.Fatalf("unmarshal lease request: %v", err)
	}
	if !proto.Equal(got, want) || got.CompletedSequence == nil ||
		got.GetCompletedSequence() != 0 {
		t.Fatalf("lease request = %+v, want %+v with explicit zero completion", got, want)
	}
	legacy := dynamicpb.NewMessage(emptyLegacyMessageDescriptor(t, "FetchStartLeaseRequest"))
	legacyWire, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy lease request: %v", err)
	}
	got = &crawlrpc.FetchStartLeaseRequest{}
	if err := proto.Unmarshal(legacyWire, got); err != nil {
		t.Fatalf("current decoder rejected legacy lease request: %v", err)
	}
	if !proto.Equal(got, &crawlrpc.FetchStartLeaseRequest{}) || got.CompletedSequence != nil {
		t.Fatalf("legacy lease request invented values: %+v", got)
	}
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy decoder rejected lease request fields: %v", err)
	}
}

func TestFetchStartLeaseDecisionWireRoundTripAndLegacyDefaults(t *testing.T) {
	want := &crawlrpc.FetchStartLeaseDecision{
		Granted: true, Sequence: 9, Permits: 23,
		FirstPermitOpensAfterNanoseconds:  -17,
		FirstPermitClosesAfterNanoseconds: 83,
		PermitIntervalNanoseconds:         100,
		RetryAfterNanoseconds:             31,
		PolicyGeneration:                  4,
		Unlimited:                         true,
	}
	wire, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal lease decision: %v", err)
	}
	got := &crawlrpc.FetchStartLeaseDecision{}
	if err := proto.Unmarshal(wire, got); err != nil {
		t.Fatalf("unmarshal lease decision: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Fatalf("lease decision = %+v, want %+v", got, want)
	}
	legacy := dynamicpb.NewMessage(emptyLegacyMessageDescriptor(t, "FetchStartLeaseDecision"))
	legacyWire, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy lease decision: %v", err)
	}
	got = &crawlrpc.FetchStartLeaseDecision{}
	if err := proto.Unmarshal(legacyWire, got); err != nil {
		t.Fatalf("current decoder rejected legacy lease decision: %v", err)
	}
	if !proto.Equal(got, &crawlrpc.FetchStartLeaseDecision{}) {
		t.Fatalf("legacy lease decision invented values: %+v", got)
	}
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy decoder rejected lease decision fields: %v", err)
	}
}

func TestFetchStartLeaseWireShapeAndRPC(t *testing.T) {
	requestFields := descriptorFieldShape(crawlrpc.File_crawlexchange_proto.Messages().
		ByName("FetchStartLeaseRequest"))
	if want := []descriptorField{
		{name: "worker_id", number: 1, kind: protoreflect.StringKind},
		{name: "worker_session_id", number: 2, kind: protoreflect.StringKind},
		{name: "sequence", number: 3, kind: protoreflect.Uint64Kind},
		{name: "maximum_permits", number: 4, kind: protoreflect.Uint32Kind},
		{name: "completed_sequence", number: 5, kind: protoreflect.Uint64Kind, presence: true},
	}; !reflect.DeepEqual(requestFields, want) {
		t.Fatalf("request wire shape = %+v, want %+v", requestFields, want)
	}
	decisionFields := descriptorFieldShape(crawlrpc.File_crawlexchange_proto.Messages().
		ByName("FetchStartLeaseDecision"))
	if want := []descriptorField{
		{name: "granted", number: 1, kind: protoreflect.BoolKind},
		{name: "sequence", number: 2, kind: protoreflect.Uint64Kind},
		{name: "permits", number: 3, kind: protoreflect.Uint32Kind},
		{name: "first_permit_opens_after_nanoseconds", number: 4, kind: protoreflect.Sint64Kind},
		{name: "first_permit_closes_after_nanoseconds", number: 5, kind: protoreflect.Sint64Kind},
		{name: "permit_interval_nanoseconds", number: 6, kind: protoreflect.Int64Kind},
		{name: "retry_after_nanoseconds", number: 7, kind: protoreflect.Int64Kind},
		{name: "policy_generation", number: 8, kind: protoreflect.Uint64Kind},
		{name: "unlimited", number: 9, kind: protoreflect.BoolKind},
	}; !reflect.DeepEqual(decisionFields, want) {
		t.Fatalf("decision wire shape = %+v, want %+v", decisionFields, want)
	}
	method := crawlrpc.File_crawlexchange_proto.Services().ByName("CrawlExchange").
		Methods().ByName("LeaseFetchStarts")
	if method == nil || method.Input().FullName() != "yacycrawl.v1.FetchStartLeaseRequest" ||
		method.Output().FullName() != "yacycrawl.v1.FetchStartLeaseDecision" ||
		method.IsStreamingClient() || method.IsStreamingServer() {
		t.Fatalf("fetch-start lease method = %+v", method)
	}
}

type descriptorField struct {
	name     protoreflect.Name
	number   protoreflect.FieldNumber
	kind     protoreflect.Kind
	presence bool
}

func descriptorFieldShape(message protoreflect.MessageDescriptor) []descriptorField {
	fields := make([]descriptorField, 0, message.Fields().Len())
	for index := range message.Fields().Len() {
		field := message.Fields().Get(index)
		fields = append(fields, descriptorField{
			name: field.Name(), number: field.Number(), kind: field.Kind(),
			presence: field.HasPresence(),
		})
	}

	return fields
}

func legacyWorkerRegistrationDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("legacy_worker_registration.proto"),
		Package: proto.String("compat"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("WorkerRegistration"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name: proto.String("worker_id"), Number: proto.Int32(1),
					Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
				{
					Name: proto.String("worker_session_id"), Number: proto.Int32(2),
					Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("legacy registration descriptor: %v", err)
	}

	return file.Messages().ByName("WorkerRegistration")
}
