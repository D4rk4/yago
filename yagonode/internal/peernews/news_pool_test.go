package peernews

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
}

func openMemPool(t *testing.T) *Pool {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	pool, err := Open(v, fixedNow)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return pool
}

func TestYaCyNewsPoolMyPublicationFixtures(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)
	originator := yagomodel.WordHash("myseed")

	for i := 1; i <= 3; i++ {
		attributes := map[string]string{
			"text":            "message " + strconv.Itoa(i),
			attributeIDOffset: strconv.Itoa(i),
		}
		if err := pool.PublishOwnNews(ctx, originator, "TestCat", attributes); err != nil {
			t.Fatalf("PublishOwnNews %d: %v", i, err)
		}
	}

	distributedIDs := map[string]bool{}
	remaining := 3*30 + 5
	record, ok, err := pool.NextPublication(ctx)
	for ok && remaining > 0 {
		if err != nil {
			t.Fatalf("NextPublication: %v", err)
		}
		if record.Distributed <= 0 {
			t.Fatalf("distributed = %d, want > 0", record.Distributed)
		}
		distributedIDs[record.ID()] = true
		remaining--
		record, ok, err = pool.NextPublication(ctx)
	}
	if err != nil {
		t.Fatalf("NextPublication: %v", err)
	}
	if remaining != 5 {
		t.Fatalf("remaining = %d, want 5 after 90 distributions", remaining)
	}
	if len(distributedIDs) != 3 {
		t.Fatalf("distributed ids = %d, want 3", len(distributedIDs))
	}

	for id := range distributedIDs {
		published, found, err := pool.ByID(ctx, Published, id)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if !found {
			t.Fatalf("news %s missing from published queue", id)
		}
		if published.Distributed != distributionLimit {
			t.Fatalf("distributed = %d, want %d", published.Distributed, distributionLimit)
		}
	}
}

func TestPublishOwnNewsSkipsDuplicateIdentity(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)
	originator := yagomodel.WordHash("myseed")

	for range 2 {
		if err := pool.PublishOwnNews(
			ctx,
			originator,
			"TestCat",
			map[string]string{"text": "same"},
		); err != nil {
			t.Fatalf("PublishOwnNews: %v", err)
		}
	}

	_, ok, err := pool.NextPublication(ctx)
	if err != nil || !ok {
		t.Fatalf("first NextPublication = %v, %v", ok, err)
	}
	if _, found, err := pool.ByID(ctx, Outgoing, ""); err != nil || found {
		t.Fatalf("ByID(empty id) = %v, %v", found, err)
	}
}

func TestPublishOwnNewsRejectsLongCategory(t *testing.T) {
	pool := openMemPool(t)

	err := pool.PublishOwnNews(
		context.Background(),
		yagomodel.WordHash("myseed"),
		"much-too-long",
		nil,
	)
	if err == nil {
		t.Fatal("long category did not fail")
	}
}

func TestPublishOwnNewsIgnoresBadIDOffset(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)

	err := pool.PublishOwnNews(
		ctx,
		yagomodel.WordHash("myseed"),
		"TestCat",
		map[string]string{attributeIDOffset: "not-a-number"},
	)
	if err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}

	record, ok, err := pool.NextPublication(ctx)
	if err != nil || !ok {
		t.Fatalf("NextPublication = %v, %v", ok, err)
	}
	if !record.Created.Equal(fixedNow()) {
		t.Fatalf("created = %v, want %v", record.Created, fixedNow())
	}
	if _, kept := record.Attributes[attributeIDOffset]; kept {
		t.Fatal("id offset attribute survived publication")
	}
}

func TestRecentReturnsNewestFirstCapped(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)
	base := fixedNow()

	for i := range 3 {
		record := Record{
			Originator: yagomodel.WordHash("peer"),
			Created:    base.Add(-time.Duration(i+1) * time.Minute),
			Received:   base.Add(-time.Duration(i+1) * time.Minute),
			Category:   CategoryCrawlStart,
			Attributes: map[string]string{"startURL": "http://example.test/" + strconv.Itoa(i)},
		}
		if stored, err := pool.EnqueueIncomingNews(ctx, record); err != nil || !stored {
			t.Fatalf("enqueue %d = %v, %v", i, stored, err)
		}
	}

	recent, err := pool.Recent(ctx, Incoming, 2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent = %d, want 2 (capped)", len(recent))
	}
	if recent[0].Attributes["startURL"] != "http://example.test/0" ||
		recent[1].Attributes["startURL"] != "http://example.test/1" {
		t.Fatalf("recent order = %q, %q; want newest first",
			recent[0].Attributes["startURL"], recent[1].Attributes["startURL"])
	}

	if got, err := pool.Recent(ctx, Incoming, 0); err != nil || got != nil {
		t.Fatalf("Recent(0) = %v, %v; want nil", got, err)
	}
	if got, err := pool.Recent(ctx, Processed, 5); err != nil || len(got) != 0 {
		t.Fatalf("Recent(empty queue) = %v, %v; want none", got, err)
	}
}

func TestEnqueueIncomingNewsValidatesAndStores(t *testing.T) {
	ctx := context.Background()
	pool := openMemPool(t)
	record := Record{
		Originator: yagomodel.WordHash("peer"),
		Created:    fixedNow(),
		Received:   fixedNow(),
		Category:   CategoryCrawlStart,
		Attributes: map[string]string{"startURL": "http://example.test/"},
	}

	stored, err := pool.EnqueueIncomingNews(ctx, record)
	if err != nil || !stored {
		t.Fatalf("EnqueueIncomingNews = %v, %v", stored, err)
	}
	if _, found, err := pool.ByID(ctx, Incoming, record.ID()); err != nil || !found {
		t.Fatalf("incoming ByID = %v, %v", found, err)
	}

	duplicate, err := pool.EnqueueIncomingNews(ctx, record)
	if err != nil || duplicate {
		t.Fatalf("duplicate = %v, %v; want rejected", duplicate, err)
	}

	unknownCategory := record
	unknownCategory.Category = "TestCat"
	stored, err = pool.EnqueueIncomingNews(ctx, unknownCategory)
	if err != nil || stored {
		t.Fatalf("unknown category = %v, %v; want rejected", stored, err)
	}

	unborn := record
	unborn.Created = time.Time{}
	stored, err = pool.EnqueueIncomingNews(ctx, unborn)
	if err != nil || stored {
		t.Fatalf("zero created = %v, %v; want rejected", stored, err)
	}
}
