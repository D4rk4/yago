package peernews

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

func TestRotateSeedNewsAttachesEncodedPublication(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)
	if err := pool.PublishOwnNews(
		ctx,
		yacymodel.WordHash("myseed"),
		CategoryProfileUpdate,
		map[string]string{"name": "yago"},
	); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}

	pool.RotateSeedNews(ctx)

	attachment := pool.SeedNews(ctx)
	if !strings.HasPrefix(attachment, "b|") {
		t.Fatalf("attachment = %q, want b| wire form", attachment)
	}
	wire, err := yacymodel.DecodeWireForm(ctx, attachment)
	if err != nil {
		t.Fatalf("DecodeWireForm: %v", err)
	}
	record, err := ParseRecord(wire, fixedNow)
	if err != nil {
		t.Fatalf("ParseRecord: %v", err)
	}
	if record.Category != CategoryProfileUpdate || record.Attributes["name"] != "yago" {
		t.Fatalf("record = %#v", record)
	}
	if record.Distributed != 1 {
		t.Fatalf("distributed = %d, want 1", record.Distributed)
	}
}

func TestRotateSeedNewsClearsAttachmentWhenQueueIsEmpty(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)
	pool.attachment.set("b|stale")

	pool.RotateSeedNews(ctx)

	if got := pool.SeedNews(ctx); got != "" {
		t.Fatalf("attachment = %q, want empty without publications", got)
	}
}

func TestRotateSeedNewsKeepsAttachmentOnStorageError(t *testing.T) {
	ctx := context.Background()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.attachment.set("b|previous")
	engine.scanErrors[queueBucket] = errors.New("scan failed")

	pool.RotateSeedNews(ctx)

	if got := pool.SeedNews(ctx); got != "b|previous" {
		t.Fatalf("attachment = %q, want previous kept on error", got)
	}
}

func TestAcceptNewsAttachmentEnqueuesIncomingRecord(t *testing.T) {
	ctx := context.Background()
	sender := openMemPool(t)
	if err := sender.PublishOwnNews(
		ctx,
		yacymodel.WordHash("peer"),
		CategoryCrawlStart,
		map[string]string{"startURL": "http://example.test/"},
	); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	sender.RotateSeedNews(ctx)

	receiver := openMemPool(t)
	receiver.AcceptNewsAttachment(ctx, sender.SeedNews(ctx))

	if _, ok, err := receiver.NextPublication(ctx); err != nil || ok {
		t.Fatalf("NextPublication = %v, %v; incoming news must not enter outgoing", ok, err)
	}

	stored, found, err := receiver.ByID(
		ctx,
		Incoming,
		mustRotatedID(t, sender),
	)
	if err != nil || !found {
		t.Fatalf("incoming ByID = %v, %v", found, err)
	}
	if stored.Category != CategoryCrawlStart {
		t.Fatalf("category = %q", stored.Category)
	}
}

func mustRotatedID(t *testing.T, pool *Pool) string {
	t.Helper()
	wire, err := yacymodel.DecodeWireForm(context.Background(), pool.SeedNews(context.Background()))
	if err != nil {
		t.Fatalf("DecodeWireForm: %v", err)
	}
	record, err := ParseRecord(wire, fixedNow)
	if err != nil {
		t.Fatalf("ParseRecord: %v", err)
	}

	return record.ID()
}

func TestAcceptNewsAttachmentIgnoresBrokenInput(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)

	pool.AcceptNewsAttachment(ctx, "")
	pool.AcceptNewsAttachment(ctx, "b|%%%not-base64%%%")
	pool.AcceptNewsAttachment(ctx, "{dis=many}")

	if _, found, err := pool.ByID(ctx, Incoming, "anything"); err != nil || found {
		t.Fatalf("incoming ByID = %v, %v; want empty queue", found, err)
	}
}

func TestAcceptNewsAttachmentReportsIntakeFailure(t *testing.T) {
	ctx := context.Background()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.putErrors[knownBucket] = errors.New("known put failed")

	record := Record{
		Originator: yacymodel.WordHash("peer"),
		Created:    fixedNow(),
		Category:   CategoryCrawlStart,
	}
	pool.AcceptNewsAttachment(ctx, yacymodel.EncodeBase64WireForm(record.WireForm()))

	if _, found, err := pool.ByID(ctx, Incoming, record.ID()); err != nil || found {
		t.Fatalf("incoming ByID = %v, %v; want rejected on storage failure", found, err)
	}
}
