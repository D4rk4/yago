package crawlsettlement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

var ErrDefinitionConflict = errors.New(
	"terminal crawl settlement definition conflicts with durable state",
)

type Outcome uint8

const (
	Delete Outcome = iota + 1
	Requeue
)

type Phase uint8

const (
	AwaitingAcknowledgment Phase = iota + 1
	AcknowledgedDeleting
	Confirming
)

type Settlement struct {
	LeaseID           string
	OrderIdentity     []byte
	Provenance        []byte
	WorkerID          string
	WorkerSessionID   string
	Outcome           Outcome
	State             yagocrawlcontract.CrawlRunState
	Tally             yagocrawlcontract.CrawlRunTally
	RecentOutcomes    yagocrawlcontract.CrawlURLOutcomeHistory
	PagesPerMinute    uint32
	RateKnown         bool
	Phase             Phase
	ConfirmationToken []byte
}

type Outbox interface {
	WorkerSessionRebinder
	Stage(context.Context, Settlement) error
	Awaiting(context.Context) ([]Settlement, error)
	Current(context.Context, string, []byte) (Settlement, bool, error)
	RecordAcknowledgment(context.Context, string, []byte, []byte) error
	PrepareConfirmation(context.Context, string, []byte) error
	Complete(context.Context, string, []byte) error
}

func Validate(settlement Settlement) bool {
	return yagocrawlcontract.ValidCrawlLeaseID(settlement.LeaseID) &&
		len(settlement.OrderIdentity) == sha256.Size &&
		len(settlement.Provenance) != 0 &&
		len(settlement.Provenance) <= yagocrawlcontract.MaximumProvenanceBytes &&
		yagocrawlcontract.ValidCrawlerWorkerIdentity(settlement.WorkerID) &&
		yagocrawlcontract.ValidCrawlerSessionIdentity(settlement.WorkerSessionID) &&
		(settlement.Outcome == Delete || settlement.Outcome == Requeue) &&
		(settlement.State == yagocrawlcontract.CrawlRunFinished ||
			settlement.State == yagocrawlcontract.CrawlRunCancelled) &&
		settlement.Tally.Pending == 0 && settlement.RecentOutcomes.Valid() &&
		validPhase(settlement)
}

func validPhase(settlement Settlement) bool {
	switch settlement.Phase {
	case 0, AwaitingAcknowledgment:
		return len(settlement.ConfirmationToken) == 0
	case AcknowledgedDeleting, Confirming:
		return len(settlement.ConfirmationToken) == sha256.Size
	default:
		return false
	}
}

func SameDefinition(left, right Settlement) bool {
	return left.LeaseID == right.LeaseID && bytes.Equal(left.OrderIdentity, right.OrderIdentity) &&
		bytes.Equal(left.Provenance, right.Provenance) &&
		left.WorkerID == right.WorkerID && left.WorkerSessionID == right.WorkerSessionID &&
		left.Outcome == right.Outcome &&
		left.State == right.State && left.Tally == right.Tally &&
		left.RecentOutcomes == right.RecentOutcomes &&
		left.PagesPerMinute == right.PagesPerMinute && left.RateKnown == right.RateKnown
}

func Clone(settlement Settlement) Settlement {
	settlement.OrderIdentity = append([]byte(nil), settlement.OrderIdentity...)
	settlement.Provenance = append([]byte(nil), settlement.Provenance...)
	settlement.ConfirmationToken = append([]byte(nil), settlement.ConfirmationToken...)

	return settlement
}
