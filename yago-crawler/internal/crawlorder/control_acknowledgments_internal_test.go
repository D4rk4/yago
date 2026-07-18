package crawlorder

import (
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestControlAcknowledgmentsRetainUnconfirmedIdentities(t *testing.T) {
	acknowledgments := &controlAcknowledgments{}
	acknowledgments.add([]uint64{2, 2, 3})
	snapshot := acknowledgments.snapshot()
	if len(snapshot) != 2 || snapshot[0] != 2 || snapshot[1] != 3 {
		t.Fatalf("snapshot = %v, want [2 3]", snapshot)
	}
	snapshot[0] = 99
	if acknowledgments.snapshot()[0] != 2 {
		t.Fatal("snapshot aliases pending acknowledgments")
	}

	acknowledgments.confirm(nil)
	acknowledgments.confirm([]uint64{2, 8})
	if remaining := acknowledgments.snapshot(); len(remaining) != 1 || remaining[0] != 3 {
		t.Fatalf("remaining = %v, want [3]", remaining)
	}
}

func TestControlAcknowledgmentSnapshotsStayWithinHeartbeatLimit(t *testing.T) {
	acknowledgments := &controlAcknowledgments{}
	identities := make(
		[]uint64,
		yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments+17,
	)
	for index := range identities {
		identities[index] = uint64(index + 1)
	}
	acknowledgments.add(identities)
	first := acknowledgments.snapshot()
	if len(first) != yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
		t.Fatalf("first acknowledgment page = %d", len(first))
	}
	acknowledgments.confirm(first)
	second := acknowledgments.snapshot()
	if len(second) != 0 || acknowledgments.available() !=
		yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
		t.Fatalf("acknowledgments after bounded storage drain = %v", second)
	}
}

func TestControlAcknowledgmentsAreConcurrentSafe(t *testing.T) {
	acknowledgments := &controlAcknowledgments{}
	var group sync.WaitGroup
	for identity := uint64(1); identity <= 64; identity++ {
		group.Go(func() {
			acknowledgments.add([]uint64{identity})
			_ = acknowledgments.snapshot()
		})
	}
	group.Wait()
	if snapshot := acknowledgments.snapshot(); len(snapshot) != 64 {
		t.Fatalf("concurrent acknowledgments = %d, want 64", len(snapshot))
	}
}
