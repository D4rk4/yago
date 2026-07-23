package crawlresults_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type profileRecorderProbe struct {
	singleProfiles int
	batchProfiles  int
	baseFetches    int
	profiles       []yagocrawlcontract.CrawlProfile
}

func (p *profileRecorderProbe) RecordFetch(
	context.Context,
	string,
	string,
	time.Time,
) error {
	p.baseFetches++

	return nil
}

func (p *profileRecorderProbe) RecordProfileFetch(
	_ context.Context,
	_ string,
	profile yagocrawlcontract.CrawlProfile,
	_ time.Time,
	_ time.Time,
) error {
	p.singleProfiles++
	p.profiles = append(p.profiles, profile)

	return nil
}

func (p *profileRecorderProbe) RecordProfileFetches(
	_ context.Context,
	_ []string,
	profiles []yagocrawlcontract.CrawlProfile,
	_ []time.Time,
	_ []time.Time,
) error {
	p.batchProfiles++
	p.profiles = append(p.profiles, profiles...)

	return nil
}

func TestSingleIngestRecordsAuthorizedLeaseProfile(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	recorder := &profileRecorderProbe{}
	consumer.RecordFetches(recorder)
	batch := fetchDocBatch("https://example.org/profile-single")
	profile := yagocrawlcontract.CrawlProfile{Handle: batch.ProfileHandle, Name: "lease profile"}
	var settled sync.WaitGroup
	settled.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch:        batch,
		CrawlProfile: &profile,
		Ack:          func(context.Context) error { settled.Done(); return nil },
		Nak:          func(context.Context) error { settled.Done(); return nil },
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go consumer.Run(ctx)
	settled.Wait()
	if recorder.singleProfiles != 1 || recorder.baseFetches != 0 ||
		len(recorder.profiles) != 1 || recorder.profiles[0].Handle != profile.Handle {
		t.Fatalf("single profile recorder = %+v", recorder)
	}
}

func TestGroupedIngestRecordsCompleteAuthorizedProfiles(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&batchRecordingReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	recorder := &profileRecorderProbe{}
	consumer.RecordFetches(recorder)
	var settled sync.WaitGroup
	counter := &settleCounter{}
	first := fetchDocBatch("https://example.org/profile-first")
	second := fetchDocBatch("https://example.org/profile-second")
	firstProfile := yagocrawlcontract.CrawlProfile{Handle: first.ProfileHandle, Name: "first"}
	secondProfile := yagocrawlcontract.CrawlProfile{Handle: second.ProfileHandle, Name: "second"}
	firstDelivery := groupDelivery(first, &settled, counter, nil)
	firstDelivery.CrawlProfile = &firstProfile
	secondDelivery := groupDelivery(second, &settled, counter, nil)
	secondDelivery.CrawlProfile = &secondProfile
	stream.out <- firstDelivery
	stream.out <- secondDelivery
	drainGroup(consumer, &settled)
	if recorder.batchProfiles != 1 || recorder.baseFetches != 0 ||
		len(recorder.profiles) != 2 {
		t.Fatalf("grouped profile recorder = %+v", recorder)
	}
}

func TestGroupedIngestFallsBackWhenProfileEvidenceIsIncomplete(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&batchRecordingReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	recorder := &profileRecorderProbe{}
	consumer.RecordFetches(recorder)
	var settled sync.WaitGroup
	counter := &settleCounter{}
	first := fetchDocBatch("https://example.org/profile-known")
	second := fetchDocBatch("https://example.org/profile-missing")
	profile := yagocrawlcontract.CrawlProfile{Handle: first.ProfileHandle, Name: "known"}
	firstDelivery := groupDelivery(first, &settled, counter, nil)
	firstDelivery.CrawlProfile = &profile
	stream.out <- firstDelivery
	stream.out <- groupDelivery(second, &settled, counter, nil)
	drainGroup(consumer, &settled)
	if recorder.batchProfiles != 0 || recorder.singleProfiles != 1 ||
		recorder.baseFetches != 1 {
		t.Fatalf("fallback profile recorder = %+v", recorder)
	}
}
