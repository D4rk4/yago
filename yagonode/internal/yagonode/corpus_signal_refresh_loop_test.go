package yagonode

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

type countedCorpus struct {
	documents []documentstore.Document
	err       error
	scans     atomic.Int32
}

func (c *countedCorpus) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	c.scans.Add(1)
	if c.err != nil {
		return c.err
	}
	for _, document := range c.documents {
		again, err := visit(document)
		if err != nil || !again {
			return err
		}
	}

	return nil
}

type gatedCorpus struct {
	documents []documentstore.Document
	started   chan int32
	release   chan struct{}
	scans     atomic.Int32
}

func (c *gatedCorpus) StoredDocuments(
	ctx context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	scan := c.scans.Add(1)
	c.started <- scan
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for corpus release: %w", ctx.Err())
	case <-c.release:
	}
	for _, document := range c.documents {
		again, err := visit(document)
		if err != nil || !again {
			return err
		}
	}

	return nil
}

type mutableHostTrustPolicy struct {
	policy  hosttrust.Policy
	changes chan struct{}
}

func (p *mutableHostTrustPolicy) Current() hosttrust.Policy { return p.policy }

func (p *mutableHostTrustPolicy) Changes() <-chan struct{} { return p.changes }

func corpusSignalDocuments() []documentstore.Document {
	return []documentstore.Document{
		{
			NormalizedURL: "https://source.example/a",
			Outlinks:      []string{"https://target.example/one"},
			Title:         "Golang tutorial Черногория",
			ExtractedText: "learn golang tutorial черногории черногорию",
		},
		{
			NormalizedURL: "https://other.example/b",
			Outlinks:      []string{"https://target.example/two"},
			Title:         "Golang guide Черногория",
			ExtractedText: "golang tutorial черногории черногорию",
		},
	}
}

func TestCorpusSignalRefreshPublishesAllSignalsFromOneScan(t *testing.T) {
	corpus := &countedCorpus{documents: corpusSignalDocuments()}
	hostRank := hostrank.NewHolder()
	spell := spellcheck.NewHolder()
	forms := wordforms.NewHolder()
	refresh := &corpusSignalRefresh{
		documents:        corpus,
		hostRank:         hostRank,
		spell:            spell,
		wordForms:        forms,
		includeWordForms: true,
	}

	refresh.scanAndPublish(t.Context())

	if got := corpus.scans.Load(); got != 1 {
		t.Fatalf("corpus scans = %d", got)
	}
	if got := hostRank.Current().Rank("target.example"); got != 1 {
		t.Fatalf("target authority = %v", got)
	}
	if got, ok := spell.Current().Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("spell suggestion = %q, %t", got, ok)
	}
	variants := forms.Current().Variants("черногория")
	if !slices.Contains(variants, "черногории") || !slices.Contains(variants, "черногорию") {
		t.Fatalf("word forms = %v", variants)
	}
	if !refresh.citationsReady || len(refresh.citations) != 2 {
		t.Fatalf("retained citations = %t, %d", refresh.citationsReady, len(refresh.citations))
	}
}

func TestCorpusSignalRefreshRetainsCitationsForTrustChanges(t *testing.T) {
	corpus := &countedCorpus{documents: []documentstore.Document{
		{NormalizedURL: "https://a.example/", Outlinks: []string{"https://b.example/"}},
		{NormalizedURL: "https://b.example/", Outlinks: []string{"https://a.example/"}},
	}}
	trust := &mutableHostTrustPolicy{changes: make(chan struct{}, 1)}
	holder := hostrank.NewHolder()
	refresh := &corpusSignalRefresh{documents: corpus, hostRank: holder, trust: trust}
	refresh.scanAndPublish(t.Context())
	if left, right := holder.Current().
		Rank("a.example"),
		holder.Current().
			Rank("b.example"); left != right {
		t.Fatalf("untrusted authority = %v, %v", left, right)
	}

	trust.policy = hosttrust.Policy{Blend: 0.5, Domains: []string{"a.example"}}
	refresh.publishAuthority(t.Context())
	if holder.Current().Rank("a.example") <= holder.Current().Rank("b.example") {
		t.Fatalf("trusted authority = %#v", holder.Current())
	}
	if got := corpus.scans.Load(); got != 1 {
		t.Fatalf("trust change corpus scans = %d", got)
	}
}

func TestCorpusSignalRefreshFailurePreservesPublishedSignals(t *testing.T) {
	hostRank := hostrank.NewHolder()
	hostRank.Store(hostrank.AuthorityTable{
		"preserved.example": {Score: 0.75, Confidence: 1},
	})
	spell := spellcheck.NewHolder()
	spell.Store(spellcheck.New(map[string]int{"golang": 5}))
	forms := wordforms.NewHolder()
	forms.Store(wordforms.New(
		map[string]int{"черногория": 5, "черногории": 3},
		searchindex.StemWord,
	))
	refresh := &corpusSignalRefresh{
		documents:        &countedCorpus{err: errors.New("scan failed")},
		hostRank:         hostRank,
		spell:            spell,
		wordForms:        forms,
		includeWordForms: true,
	}

	refresh.scanAndPublish(t.Context())

	if got := hostRank.Current().Rank("preserved.example"); got != 0.75 {
		t.Fatalf("preserved authority = %v", got)
	}
	if got, ok := spell.Current().Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("preserved spell suggestion = %q, %t", got, ok)
	}
	if got := forms.Current().Variants("черногория"); !slices.Contains(got, "черногории") {
		t.Fatalf("preserved word forms = %v", got)
	}
	if refresh.citationsReady {
		t.Fatal("failed scan replaced retained citations")
	}
}

func TestCorpusSignalRefreshHandlesMissingOutputsAndCancellation(t *testing.T) {
	refresh := &corpusSignalRefresh{
		documents:        &countedCorpus{documents: corpusSignalDocuments()},
		includeWordForms: true,
	}
	refresh.scanAndPublish(t.Context())
	if !refresh.citationsReady {
		t.Fatal("nil outputs prevented citation retention")
	}
	refresh.publishAuthority(t.Context())

	forms := wordforms.NewHolder()
	withoutForms := &corpusSignalRefresh{
		documents: &countedCorpus{documents: corpusSignalDocuments()},
		wordForms: forms,
	}
	withoutForms.scanAndPublish(t.Context())
	if got := forms.Current().Variants("черногория"); len(got) != 1 {
		t.Fatalf("disabled word forms = %v", got)
	}

	holder := hostrank.NewHolder()
	canceled := &corpusSignalRefresh{
		documents: &countedCorpus{documents: corpusSignalDocuments()},
		hostRank:  holder,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceled.scanAndPublish(ctx)
	if len(holder.Current()) != 0 || canceled.citationsReady {
		t.Fatalf("canceled refresh published state: %#v", holder.Current())
	}
	canceled.citationsReady = true
	canceled.citations = []hostrank.Citation{{
		SourceURL: "https://source.example/", TargetURL: "https://target.example/", Confidence: 1,
	}}
	canceled.publishAuthority(ctx)

	(&corpusSignalRefresh{}).scanAndPublish(t.Context())
}

func TestCorpusSignalRefreshLoopSchedulesAfterScanCompletion(t *testing.T) {
	oldDelay := newCorpusSignalRefreshDelay
	t.Cleanup(func() { newCorpusSignalRefreshDelay = oldDelay })
	created := make(chan chan time.Time, 4)
	durations := make(chan time.Duration, 4)
	var stopped atomic.Int32
	newCorpusSignalRefreshDelay = func(interval time.Duration) (<-chan time.Time, func()) {
		ticks := make(chan time.Time, 1)
		durations <- interval
		created <- ticks

		return ticks, func() { stopped.Add(1) }
	}

	corpus := &gatedCorpus{
		documents: corpusSignalDocuments(),
		started:   make(chan int32, 2),
		release:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		runCorpusSignalRefreshLoop(ctx, &corpusSignalRefresh{documents: corpus})
		close(done)
	}()

	if scan := <-corpus.started; scan != 1 {
		t.Fatalf("first scan = %d", scan)
	}
	select {
	case <-created:
		t.Fatal("refresh delay started before scan completion")
	default:
	}
	corpus.release <- struct{}{}
	firstTicks := <-created
	if interval := <-durations; interval != defaultCorpusSignalRefreshInterval {
		t.Fatalf("refresh interval = %v", interval)
	}
	firstTicks <- time.Now()
	if scan := <-corpus.started; scan != 2 {
		t.Fatalf("second scan = %d", scan)
	}
	select {
	case <-created:
		t.Fatal("next refresh delay started before second scan completion")
	default:
	}
	corpus.release <- struct{}{}
	<-created
	<-durations
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh loop did not stop")
	}
	if got := stopped.Load(); got != 2 {
		t.Fatalf("stopped delays = %d", got)
	}

	canceledContext, cancelImmediately := context.WithCancel(t.Context())
	cancelImmediately()
	noScan := &countedCorpus{}
	runCorpusSignalRefreshLoop(canceledContext, &corpusSignalRefresh{documents: noScan})
	if got := noScan.scans.Load(); got != 0 {
		t.Fatalf("canceled loop scans = %d", got)
	}
}

func TestCorpusSignalRefreshWaitHandlesTrustAndClosedSources(t *testing.T) {
	oldDelay := newCorpusSignalRefreshDelay
	t.Cleanup(func() { newCorpusSignalRefreshDelay = oldDelay })
	created := make(chan chan time.Time, 2)
	newCorpusSignalRefreshDelay = func(time.Duration) (<-chan time.Time, func()) {
		ticks := make(chan time.Time, 1)
		created <- ticks

		return ticks, func() {}
	}

	changes := make(chan struct{}, 1)
	holder := hostrank.NewHolder()
	refresh := &corpusSignalRefresh{
		hostRank: holder,
		trust: &mutableHostTrustPolicy{
			policy:  hosttrust.Policy{Blend: 0.5, Domains: []string{"a.example"}},
			changes: changes,
		},
		citations: []hostrank.Citation{
			{SourceURL: "https://a.example/", TargetURL: "https://b.example/", Confidence: 1},
			{SourceURL: "https://b.example/", TargetURL: "https://a.example/", Confidence: 1},
		},
		citationsReady: true,
	}
	waited := make(chan bool, 1)
	go func() { waited <- refresh.wait(t.Context()) }()
	ticks := <-created
	changes <- struct{}{}
	deadline := time.Now().Add(time.Second)
	for holder.Current().Rank("a.example") <= holder.Current().Rank("b.example") {
		if time.Now().After(deadline) {
			t.Fatal("trust event did not publish retained authority")
		}
		time.Sleep(time.Millisecond)
	}
	ticks <- time.Now()
	if didWait := <-waited; !didWait {
		t.Fatal("refresh delay did not complete")
	}

	closedChanges := make(chan struct{})
	close(closedChanges)
	closedWait := &corpusSignalRefresh{
		trust: &mutableHostTrustPolicy{changes: closedChanges},
	}
	go func() { waited <- closedWait.wait(t.Context()) }()
	closedTicks := <-created
	closedTicks <- time.Now()
	if didWait := <-waited; !didWait {
		t.Fatal("closed trust source stopped refresh delay")
	}
}

func TestDefaultCorpusSignalRefreshDelayFires(t *testing.T) {
	delay, stop := newCorpusSignalRefreshDelay(0)
	defer stop()
	select {
	case <-delay:
	case <-time.After(time.Second):
		t.Fatal("refresh delay did not fire")
	}
}
