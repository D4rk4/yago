package crawlsettlement

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func validTestSettlement() Settlement {
	return Settlement{
		LeaseID:         "lease",
		OrderIdentity:   bytes.Repeat([]byte{1}, sha256.Size),
		Provenance:      []byte("run"),
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: 1},
	}
}

func TestTerminalSettlementValidationCoversEveryDurablePhase(t *testing.T) {
	settlement := validTestSettlement()
	if !Validate(settlement) {
		t.Fatal("unstaged terminal settlement is invalid")
	}
	settlement.Phase = AwaitingAcknowledgment
	if !Validate(settlement) {
		t.Fatal("awaiting terminal settlement is invalid")
	}
	settlement.Phase = AcknowledgedDeleting
	if Validate(settlement) {
		t.Fatal("acknowledged settlement without token is valid")
	}
	settlement.ConfirmationToken = bytes.Repeat([]byte{2}, sha256.Size)
	if !Validate(settlement) {
		t.Fatal("acknowledged terminal settlement is invalid")
	}
	settlement.Phase = Confirming
	if !Validate(settlement) {
		t.Fatal("confirming terminal settlement is invalid")
	}
	settlement.Phase++
	if Validate(settlement) {
		t.Fatal("unknown terminal settlement phase is valid")
	}
}

func TestTerminalSettlementDefinitionIsExactAndCloned(t *testing.T) {
	original := validTestSettlement()
	copy := Clone(original)
	if !SameDefinition(original, copy) {
		t.Fatal("cloned terminal settlement definition changed")
	}
	copy.OrderIdentity[0]++
	copy.Provenance[0]++
	if original.OrderIdentity[0] != 1 || string(original.Provenance) != "run" {
		t.Fatal("terminal settlement clone aliases definition bytes")
	}
	mutations := []func(*Settlement){
		func(value *Settlement) { value.LeaseID += "-other" },
		func(value *Settlement) { value.OrderIdentity[0]++ },
		func(value *Settlement) { value.Provenance[0]++ },
		func(value *Settlement) { value.WorkerID += "-other" },
		func(value *Settlement) { value.WorkerSessionID += "-other" },
		func(value *Settlement) { value.Outcome = Requeue },
		func(value *Settlement) { value.State = yagocrawlcontract.CrawlRunCancelled },
		func(value *Settlement) { value.Tally.Fetched++ },
		func(value *Settlement) { value.PagesPerMinute++ },
		func(value *Settlement) { value.RateKnown = true },
	}
	for index, mutate := range mutations {
		changed := Clone(original)
		mutate(&changed)
		if SameDefinition(original, changed) {
			t.Fatalf("mutation %d preserved terminal definition", index)
		}
	}
	phaseChanged := Clone(original)
	phaseChanged.Phase = Confirming
	phaseChanged.ConfirmationToken = bytes.Repeat([]byte{3}, sha256.Size)
	if !SameDefinition(original, phaseChanged) {
		t.Fatal("delivery phase changed terminal definition")
	}
}

func TestTerminalSettlementRejectsIncompleteDefinitions(t *testing.T) {
	valid := validTestSettlement()
	mutations := []func(*Settlement){
		func(value *Settlement) { value.LeaseID = "" },
		func(value *Settlement) { value.OrderIdentity = nil },
		func(value *Settlement) { value.Provenance = nil },
		func(value *Settlement) { value.WorkerID = "" },
		func(value *Settlement) { value.WorkerSessionID = "" },
		func(value *Settlement) {
			value.LeaseID = strings.Repeat("l", yagocrawlcontract.MaximumCrawlLeaseIDBytes+1)
		},
		func(value *Settlement) {
			value.Provenance = bytes.Repeat(
				[]byte{'p'},
				yagocrawlcontract.MaximumProvenanceBytes+1,
			)
		},
		func(value *Settlement) {
			value.WorkerID = strings.Repeat(
				"w",
				yagocrawlcontract.MaximumCrawlerWorkerIdentityBytes+1,
			)
		},
		func(value *Settlement) {
			value.WorkerSessionID = strings.Repeat(
				"s",
				yagocrawlcontract.MaximumCrawlerSessionIdentityBytes+1,
			)
		},
		func(value *Settlement) { value.LeaseID = string([]byte{0xff}) },
		func(value *Settlement) { value.WorkerID = string([]byte{0xff}) },
		func(value *Settlement) { value.WorkerSessionID = string([]byte{0xff}) },
		func(value *Settlement) { value.Outcome = 0 },
		func(value *Settlement) { value.State = yagocrawlcontract.CrawlRunRunning },
		func(value *Settlement) { value.Tally.Pending = 1 },
	}
	for index, mutate := range mutations {
		changed := Clone(valid)
		mutate(&changed)
		if Validate(changed) {
			t.Fatalf("invalid definition %d passed validation", index)
		}
	}
}
