package crawlresults

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type outboundAnchorStageRecorder struct {
	observed map[IngestStage][]time.Duration
}

func (recorder *outboundAnchorStageRecorder) ObserveIngestStage(
	stage IngestStage,
	elapsed time.Duration,
) {
	if recorder.observed == nil {
		recorder.observed = make(map[IngestStage][]time.Duration)
	}
	recorder.observed[stage] = append(recorder.observed[stage], elapsed)
}

type measuredOutboundAnchorReceiver struct {
	documents []documentstore.Document
}

func (receiver *measuredOutboundAnchorReceiver) ReplaceOutboundAnchors(
	context.Context,
	[]documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	return documentstore.AnchorUpdate{}, nil
}

func (receiver *measuredOutboundAnchorReceiver) VisitOutboundAnchorDocuments(
	_ context.Context,
	_ []documentstore.OutboundAnchorFinalization,
	visit func([]documentstore.Document) error,
) error {
	return visit(receiver.documents)
}

func (*measuredOutboundAnchorReceiver) FinalizeOutboundAnchors(
	context.Context,
	[]documentstore.OutboundAnchorFinalization,
) error {
	return nil
}

func (*measuredOutboundAnchorReceiver) ReleaseOutboundAnchors(
	[]documentstore.OutboundAnchorFinalization,
) {
}

func (*measuredOutboundAnchorReceiver) Receive(
	context.Context,
	[]documentstore.Document,
) (documentstore.Receipt, error) {
	return documentstore.Receipt{}, nil
}

func (receiver *measuredOutboundAnchorReceiver) ReplaceOutboundAnchorDocuments(
	_ context.Context,
	_ []documentstore.OutboundAnchorSet,
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	return documentstore.AnchorReplacementReceipt{}, visit(receiver.documents)
}

func TestOutboundAnchorReplacementAndIndexStagesAreMeasured(t *testing.T) {
	batch := yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL:               "https://source.example/",
		OutboundAnchorEvidenceKnown: true,
	}}
	deliveries := []IngestDelivery{{Batch: batch}}
	receiver := &measuredOutboundAnchorReceiver{documents: []documentstore.Document{{
		NormalizedURL: "https://target.example/",
	}}}
	stages := &outboundAnchorStageRecorder{}
	consumer := &IngestConsumer{
		anchors:      receiver,
		index:        &anchorIndexScript{},
		ingestStages: stages,
	}
	if deferred := consumer.updateInboundAnchors(context.Background(), deliveries); deferred {
		t.Fatal("successful anchor update was deferred")
	}
	if len(stages.observed[IngestStageOutboundAnchorDocumentReplacement]) != 1 ||
		len(stages.observed[IngestStageOutboundAnchorIndexCallback]) != 1 {
		t.Fatalf("anchor stages = %#v", stages.observed)
	}

	stages = &outboundAnchorStageRecorder{}
	consumer.ingestStages = stages
	consumer.index = nil
	if deferred := consumer.updateInboundAnchors(context.Background(), deliveries); deferred {
		t.Fatal("anchor update without an index was deferred")
	}
	if len(stages.observed[IngestStageOutboundAnchorDocumentReplacement]) != 1 ||
		len(stages.observed[IngestStageOutboundAnchorIndexCallback]) != 0 {
		t.Fatalf("anchor stages without index = %#v", stages.observed)
	}
}
