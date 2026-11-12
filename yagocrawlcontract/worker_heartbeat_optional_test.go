package yagocrawlcontract_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestWorkerHeartbeatAdmissionWaitFieldPreservesPresenceAndWireCompatibility(t *testing.T) {
	absent := &crawlrpc.WorkerHeartbeat{WorkerId: "worker"}
	absentBytes, err := proto.Marshal(absent)
	if err != nil {
		t.Fatalf("marshal absent heartbeat: %v", err)
	}
	decodedAbsent := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(absentBytes, decodedAbsent); err != nil {
		t.Fatalf("unmarshal absent heartbeat: %v", err)
	}
	if decodedAbsent.FetchAdmissionWaiting != nil {
		t.Fatalf("absent field decoded as %v, want nil", decodedAbsent.FetchAdmissionWaiting)
	}

	zero := uint32(0)
	current := &crawlrpc.WorkerHeartbeat{WorkerId: "worker", FetchAdmissionWaiting: &zero}
	currentBytes, err := proto.Marshal(current)
	if err != nil {
		t.Fatalf("marshal present zero heartbeat: %v", err)
	}
	decodedCurrent := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(currentBytes, decodedCurrent); err != nil {
		t.Fatalf("unmarshal present zero heartbeat: %v", err)
	}
	if decodedCurrent.FetchAdmissionWaiting == nil ||
		decodedCurrent.GetFetchAdmissionWaiting() != 0 {
		t.Fatalf(
			"present zero decoded as %v, want present zero",
			decodedCurrent.FetchAdmissionWaiting,
		)
	}

	newField := currentBytes[len(absentBytes):]
	fieldNumber, wireType, size := protowire.ConsumeTag(newField)
	if size < 0 || fieldNumber != 12 || wireType != protowire.VarintType {
		t.Fatalf("new heartbeat field tag = %d/%d, want field 12 varint", fieldNumber, wireType)
	}
	value, valueSize := protowire.ConsumeVarint(newField[size:])
	if valueSize < 0 || value != 0 {
		t.Fatalf("present zero wire value = %d, want 0", value)
	}
	legacyWorkerID := ""
	for wire := currentBytes; len(wire) > 0; {
		field, kind, tagSize := protowire.ConsumeTag(wire)
		if tagSize < 0 {
			t.Fatalf("legacy decoder could not read field tag: %v", protowire.ParseError(tagSize))
		}
		wire = wire[tagSize:]
		if field == 1 {
			legacyWorkerID, tagSize = protowire.ConsumeString(wire)
		} else {
			tagSize = protowire.ConsumeFieldValue(field, kind, wire)
		}
		if tagSize < 0 {
			t.Fatalf(
				"legacy decoder could not skip field %d: %v",
				field,
				protowire.ParseError(tagSize),
			)
		}
		wire = wire[tagSize:]
	}
	if legacyWorkerID != "worker" {
		t.Fatalf(
			"legacy heartbeat worker id = %q, want worker after ignoring field 12",
			legacyWorkerID,
		)
	}
}
