package recrawlfrontier

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

func TestRecordProfileFetchPersistsScheduleAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	frontier, err := Open(storage)
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	profile := profileWithRecrawl("Lease profile", time.Hour)
	if err := frontier.RecordProfileFetch(
		t.Context(),
		"https://restart.example/",
		profile,
		testBase,
		time.Time{},
	); err != nil {
		t.Fatalf("record profile fetch: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restored, err := Open(reopened)
	if err != nil {
		t.Fatalf("reopen frontier: %v", err)
	}
	stored, found, err := restored.ProfileByHandle(context.Background(), profile.Handle)
	if err != nil {
		t.Fatalf("read restored profile: %v", err)
	}
	if !found || stored.Handle != profile.Handle {
		t.Fatalf("restored profile = %+v, found %t", stored, found)
	}
	due := claim(t, restored, testBase.Add(time.Hour), 1)
	if len(due) != 1 || due[0].URL != "https://restart.example/" {
		t.Fatalf("restored due URLs = %+v", due)
	}
}

func TestRecordProfileFetchesRejectsInvalidEvidence(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := profileWithRecrawl("Lease profile", time.Hour)
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://length.example/"},
		nil,
		[]time.Time{testBase},
		[]time.Time{{}},
	); err == nil {
		t.Fatal("mismatched profile evidence was accepted")
	}
	profile.Handle = ""
	if err := frontier.RecordProfileFetch(
		t.Context(),
		"https://handle.example/",
		profile,
		testBase,
		time.Time{},
	); err == nil {
		t.Fatal("empty profile handle was accepted")
	}
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://batch-handle.example/"},
		[]yagocrawlcontract.CrawlProfile{{}},
		[]time.Time{testBase},
		[]time.Time{{}},
	); err == nil {
		t.Fatal("empty batch profile handle was accepted")
	}
}

func TestRecordProfileFetchPersistsProfileBeforeScheduleFailure(t *testing.T) {
	frontier, engine := openCtrlFrontier(t)
	profile := profileWithRecrawl("Lease profile", time.Hour)
	engine.failPut[recordBucket] = true
	if err := frontier.RecordProfileFetch(
		t.Context(),
		"https://schedule-failure.example/",
		profile,
		testBase,
		time.Time{},
	); err == nil {
		t.Fatal("schedule failure was hidden")
	}
	stored, found, err := frontier.ProfileByHandle(t.Context(), profile.Handle)
	if err != nil {
		t.Fatalf("read persisted profile: %v", err)
	}
	if !found || stored.Handle != profile.Handle {
		t.Fatalf("persisted profile = %+v, found %t", stored, found)
	}
}

func TestRecordProfileFetchSurfacesProfilePersistenceFailures(t *testing.T) {
	profile := profileWithRecrawl("Lease profile", time.Hour)
	t.Run("write", func(t *testing.T) {
		frontier, engine := openCtrlFrontier(t)
		engine.failPut[profileBucket] = true
		if err := frontier.RecordProfileFetch(
			t.Context(),
			"https://profile-write.example/",
			profile,
			testBase,
			time.Time{},
		); err == nil {
			t.Fatal("profile write failure was hidden")
		}
	})
	t.Run("read", func(t *testing.T) {
		frontier, engine := openCtrlFrontier(t)
		engine.seed(profileBucket, profile.Handle, []byte("{"))
		if err := frontier.RecordProfileFetch(
			t.Context(),
			"https://profile-read.example/",
			profile,
			testBase,
			time.Time{},
		); err == nil {
			t.Fatal("profile read failure was hidden")
		}
	})
	t.Run("context", func(t *testing.T) {
		frontier := openTestFrontier(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := frontier.RecordProfileFetch(
			ctx,
			"https://profile-context.example/",
			profile,
			testBase,
			time.Time{},
		); err == nil {
			t.Fatal("profile context failure was hidden")
		}
	})
}

func TestRecordProfileFetchesSurfacesScheduleFailure(t *testing.T) {
	frontier, engine := openCtrlFrontier(t)
	profile := profileWithRecrawl("Lease profile", time.Hour)
	if err := frontier.RecordProfile(t.Context(), profile); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	engine.failPut[recordBucket] = true
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://batch-schedule-failure.example/"},
		[]yagocrawlcontract.CrawlProfile{profile},
		[]time.Time{testBase},
		[]time.Time{{}},
	); err == nil {
		t.Fatal("batch schedule failure was hidden")
	}
}

func TestRecordProfileFetchesPersistsProfilesAndSchedules(t *testing.T) {
	frontier := openTestFrontier(t)
	first := profileWithRecrawl("First lease profile", time.Hour)
	second := profileWithRecrawl("Second lease profile", 2*time.Hour)
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://first.example/", "https://second.example/"},
		[]yagocrawlcontract.CrawlProfile{first, second},
		[]time.Time{testBase, testBase},
		[]time.Time{{}, {}},
	); err != nil {
		t.Fatalf("record profile fetches: %v", err)
	}
	for _, profile := range []yagocrawlcontract.CrawlProfile{first, second} {
		stored, found, err := frontier.ProfileByHandle(t.Context(), profile.Handle)
		if err != nil {
			t.Fatalf("read profile %q: %v", profile.Name, err)
		}
		if !found || stored.Handle != profile.Handle {
			t.Fatalf("stored profile %q = %+v, found %t", profile.Name, stored, found)
		}
	}
	firstDue := claim(t, frontier, testBase.Add(time.Hour), 10)
	if len(firstDue) != 1 || firstDue[0].URL != "https://first.example/" {
		t.Fatalf("first due URLs = %+v", firstDue)
	}
	secondDue := claim(t, frontier, testBase.Add(2*time.Hour), 10)
	if len(secondDue) != 2 {
		t.Fatalf("second due URLs = %+v", secondDue)
	}
}

func TestRecordProfileFetchesWritesEachMissingProfileOnce(t *testing.T) {
	frontier, engine := openCtrlFrontier(t)
	profile := profileWithRecrawl("Shared lease profile", time.Hour)
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://first.example/", "https://second.example/"},
		[]yagocrawlcontract.CrawlProfile{profile, profile},
		[]time.Time{testBase, testBase},
		[]time.Time{{}, {}},
	); err != nil {
		t.Fatalf("record repeated profile fetches: %v", err)
	}
	if writes := engine.putCalls[profileBucket]; writes != 1 {
		t.Fatalf("profile writes = %d, want 1", writes)
	}
	engine.failPut[profileBucket] = true
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://third.example/"},
		[]yagocrawlcontract.CrawlProfile{profile},
		[]time.Time{testBase},
		[]time.Time{{}},
	); err != nil {
		t.Fatalf("record unchanged profile fetch: %v", err)
	}
	profile.RecrawlIfOlder = 2 * time.Hour
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://fourth.example/"},
		[]yagocrawlcontract.CrawlProfile{profile},
		[]time.Time{testBase},
		[]time.Time{{}},
	); err != nil {
		t.Fatalf("record changed profile fetch: %v", err)
	}
	if writes := engine.putCalls[profileBucket]; writes != 1 {
		t.Fatalf("existing profile writes = %d, want 1", writes)
	}
}

func TestRecordProfileFetchesKeepsEachLiveLeaseInterval(t *testing.T) {
	frontier := openTestFrontier(t)
	first := profileWithRecrawl("Shared lease profile", time.Hour)
	second := first
	second.RecrawlIfOlder = 2 * time.Hour
	if err := frontier.RecordProfileFetches(
		t.Context(),
		[]string{"https://first-lease.example/", "https://second-lease.example/"},
		[]yagocrawlcontract.CrawlProfile{first, second},
		[]time.Time{testBase, testBase},
		[]time.Time{{}, {}},
	); err != nil {
		t.Fatalf("record mixed live lease profiles: %v", err)
	}
	firstDue := claim(t, frontier, testBase.Add(time.Hour), 10)
	if len(firstDue) != 1 || firstDue[0].URL != "https://first-lease.example/" {
		t.Fatalf("first live lease due URLs = %+v", firstDue)
	}
	secondDue := claim(t, frontier, testBase.Add(2*time.Hour), 10)
	if len(secondDue) != 2 {
		t.Fatalf("second live lease due URLs = %+v", secondDue)
	}
}

func TestOlderIngestDoesNotReplaceLatestDispatchedProfile(t *testing.T) {
	frontier := openTestFrontier(t)
	older := profileWithRecrawl("Shared profile", time.Hour)
	latest := older
	latest.RecrawlIfOlder = 2 * time.Hour
	if err := frontier.RecordProfile(t.Context(), latest); err != nil {
		t.Fatalf("record latest profile: %v", err)
	}
	if err := frontier.RecordProfileFetch(
		t.Context(),
		"https://interleaved.example/",
		older,
		testBase,
		time.Time{},
	); err != nil {
		t.Fatalf("record older ingest: %v", err)
	}
	stored, found, err := frontier.ProfileByHandle(t.Context(), older.Handle)
	if err != nil {
		t.Fatalf("read registered profile: %v", err)
	}
	if !found || stored.RecrawlIfOlder != latest.RecrawlIfOlder {
		t.Fatalf("registered profile = %+v, want latest dispatch", stored)
	}
	due := claim(t, frontier, testBase.Add(time.Hour), 1)
	if len(due) != 1 || due[0].URL != "https://interleaved.example/" {
		t.Fatalf("live lease schedule = %+v, want older fetch interval", due)
	}
}
