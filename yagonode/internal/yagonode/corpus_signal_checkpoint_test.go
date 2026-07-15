package yagonode

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/corpussignals"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

type memoryCorpusSignalCheckpoints struct {
	mu           sync.Mutex
	checkpoint   corpussignals.Checkpoint
	found        bool
	loadError    error
	replaceError error
	attempts     int
	replacements []corpussignals.Checkpoint
}

type changingCorpusSignalTrustPolicy struct {
	calls atomic.Int32
	first hosttrust.Policy
	next  hosttrust.Policy
}

func (p *changingCorpusSignalTrustPolicy) Current() hosttrust.Policy {
	if p.calls.Add(1) == 1 {
		return p.first
	}

	return p.next
}

func (*changingCorpusSignalTrustPolicy) Changes() <-chan struct{} { return nil }

func (m *memoryCorpusSignalCheckpoints) Load(
	context.Context,
) (corpussignals.Checkpoint, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return copyCorpusSignalCheckpoint(m.checkpoint), m.found, m.loadError
}

func (m *memoryCorpusSignalCheckpoints) Replace(
	_ context.Context,
	checkpoint corpussignals.Checkpoint,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts++
	if m.replaceError != nil {
		return m.replaceError
	}
	m.checkpoint = copyCorpusSignalCheckpoint(checkpoint)
	m.found = true
	m.replacements = append(m.replacements, copyCorpusSignalCheckpoint(checkpoint))

	return nil
}

func (m *memoryCorpusSignalCheckpoints) snapshot() (corpussignals.Checkpoint, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return copyCorpusSignalCheckpoint(m.checkpoint), m.attempts, len(m.replacements)
}

func copyCorpusSignalCheckpoint(checkpoint corpussignals.Checkpoint) corpussignals.Checkpoint {
	authority := make(hostrank.AuthorityTable, len(checkpoint.Authority))
	for domain, evidence := range checkpoint.Authority {
		authority[domain] = evidence
	}
	citations := append([]hostrank.Citation(nil), checkpoint.Citations...)
	spelling := make(map[string]int, len(checkpoint.Spelling))
	for term, frequency := range checkpoint.Spelling {
		spelling[term] = frequency
	}
	forms := make(map[string]int, len(checkpoint.WordForms))
	for term, frequency := range checkpoint.WordForms {
		forms[term] = frequency
	}
	domains := append([]string(nil), checkpoint.TrustDomains...)

	return corpussignals.Checkpoint{
		Authority: authority, Citations: citations, Spelling: spelling, WordForms: forms,
		WordFormsReady: checkpoint.WordFormsReady,
		HostLinks:      hostlinks.CloneGraph(checkpoint.HostLinks),
		HostLinksReady: checkpoint.HostLinksReady, TrustDomains: domains,
		TrustBlend: checkpoint.TrustBlend, CompletedAtUnixMilli: checkpoint.CompletedAtUnixMilli,
	}
}

func restoredCorpusSignalCheckpoint(completedAt time.Time) corpussignals.Checkpoint {
	return corpussignals.Checkpoint{
		Authority: hostrank.AuthorityTable{
			"source.example": {Score: 0.2, Confidence: 0.5},
			"target.example": {Score: 1, Confidence: 1},
		},
		Citations: []hostrank.Citation{{
			SourceURL:  "https://source.example/page",
			TargetURL:  "https://target.example/page",
			Confidence: 1,
		}},
		Spelling:             map[string]int{"golang": 4},
		WordForms:            map[string]int{"черногория": 4, "черногории": 3},
		WordFormsReady:       true,
		HostLinks:            hostlinks.Graph{RowDefinition: hostlinks.HostReferenceRowDefinition},
		HostLinksReady:       true,
		TrustDomains:         []string{},
		CompletedAtUnixMilli: completedAt.UnixMilli(),
	}
}

func TestCorpusSignalCheckpointRestoresSignalsAndDefersUntilDue(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repository := &memoryCorpusSignalCheckpoints{
		checkpoint: restoredCorpusSignalCheckpoint(now.Add(-3 * time.Minute)),
		found:      true,
	}
	corpus := &countedCorpus{documents: corpusSignalDocuments()}
	hostRank := hostrank.NewHolder()
	spell := spellcheck.NewHolder()
	forms := wordforms.NewHolder()
	hostLinkSnapshot := hostlinks.NewSnapshotHolder()
	refresh := &corpusSignalRefresh{
		documents: corpus, hostRank: hostRank, spell: spell, wordForms: forms,
		hostLinks:        hostLinkSnapshot,
		includeWordForms: true, checkpoints: repository, readTime: func() time.Time { return now },
	}
	refresh.initialRefreshDelay = refresh.restoreCheckpoint(t.Context())

	if refresh.initialRefreshDelay != 7*time.Minute {
		t.Fatalf("initial refresh delay = %v", refresh.initialRefreshDelay)
	}
	assertRestoredCorpusSignals(t, hostRank, spell, forms, hostLinkSnapshot)
	if corpus.scans.Load() != 0 {
		t.Fatal("fresh checkpoint triggered an eager scan")
	}

	oldDelay := newCorpusSignalRefreshDelay
	t.Cleanup(func() { newCorpusSignalRefreshDelay = oldDelay })
	created := make(chan chan time.Time, 2)
	durations := make(chan time.Duration, 2)
	newCorpusSignalRefreshDelay = func(interval time.Duration) (<-chan time.Time, func()) {
		ticks := make(chan time.Time, 1)
		durations <- interval
		created <- ticks

		return ticks, func() {}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		runCorpusSignalRefreshLoop(ctx, refresh)
		close(done)
	}()
	initialTicks := <-created
	if duration := <-durations; duration != 7*time.Minute {
		t.Fatalf("scheduled initial delay = %v", duration)
	}
	if corpus.scans.Load() != 0 {
		t.Fatal("scan started before restored checkpoint was due")
	}
	initialTicks <- now
	deadline := time.Now().Add(time.Second)
	for corpus.scans.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("due checkpoint did not refresh")
		}
		time.Sleep(time.Millisecond)
	}
	<-created
	if duration := <-durations; duration != defaultCorpusSignalRefreshInterval {
		t.Fatalf("scheduled recurring delay = %v", duration)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("restored refresh loop did not stop")
	}
}

func assertRestoredCorpusSignals(
	t *testing.T,
	hostRank *hostrank.Holder,
	spell *spellcheck.Holder,
	forms *wordforms.Holder,
	hostLinkSnapshot *hostlinks.SnapshotHolder,
) {
	t.Helper()
	if got := hostRank.Current().Rank("target.example"); got != 1 {
		t.Fatalf("restored authority = %v", got)
	}
	if got, ok := spell.Current().Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("restored spelling = %q, %t", got, ok)
	}
	if got := forms.Current().Variants("черногория"); !slices.Contains(got, "черногории") {
		t.Fatalf("restored word forms = %v", got)
	}
	if got := hostLinkSnapshot.IncomingHostLinks(
		t.Context(),
	); got.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("restored host links = %#v", got)
	}
}

func TestCorpusSignalCheckpointPublishesStaleButRefreshesImmediately(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for _, completedAt := range []time.Time{
		now.Add(-defaultCorpusSignalRefreshInterval),
		now.Add(time.Millisecond),
	} {
		repository := &memoryCorpusSignalCheckpoints{
			checkpoint: restoredCorpusSignalCheckpoint(completedAt),
			found:      true,
		}
		holder := hostrank.NewHolder()
		refresh := &corpusSignalRefresh{
			hostRank: holder, checkpoints: repository, readTime: func() time.Time { return now },
		}
		if delay := refresh.restoreCheckpoint(t.Context()); delay != 0 {
			t.Fatalf("stale checkpoint delay = %v", delay)
		}
		if got := holder.Current().Rank("target.example"); got != 1 {
			t.Fatalf("stale restored authority = %v", got)
		}
	}

	missingForms := restoredCorpusSignalCheckpoint(now.Add(-time.Minute))
	missingForms.WordForms = map[string]int{}
	missingForms.WordFormsReady = false
	formsRepository := &memoryCorpusSignalCheckpoints{checkpoint: missingForms, found: true}
	spell := spellcheck.NewHolder()
	forms := wordforms.NewHolder()
	refresh := &corpusSignalRefresh{
		spell: spell, wordForms: forms, includeWordForms: true,
		checkpoints: formsRepository, readTime: func() time.Time { return now },
	}
	if delay := refresh.restoreCheckpoint(t.Context()); delay != 0 {
		t.Fatalf("incomplete morphology checkpoint delay = %v", delay)
	}
	if got, ok := spell.Current().Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("restored spelling without forms = %q, %t", got, ok)
	}
	if got := forms.Current().
		Variants("черногория"); !reflect.DeepEqual(
		got,
		[]string{"черногория"},
	) {
		t.Fatalf("unavailable restored word forms = %v", got)
	}

	missingLinks := restoredCorpusSignalCheckpoint(now.Add(-time.Minute))
	missingLinks.HostLinks = hostlinks.Graph{}
	missingLinks.HostLinksReady = false
	linksRepository := &memoryCorpusSignalCheckpoints{checkpoint: missingLinks, found: true}
	links := hostlinks.NewSnapshotHolder()
	refresh = &corpusSignalRefresh{
		hostLinks: links, checkpoints: linksRepository, readTime: func() time.Time { return now },
	}
	if delay := refresh.restoreCheckpoint(t.Context()); delay != 0 {
		t.Fatalf("incomplete host-link checkpoint delay = %v", delay)
	}
	if got := links.IncomingHostLinks(
		t.Context(),
	); got.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("unavailable restored host links = %#v", got)
	}
}

func TestCorpusSignalCheckpointRecomputesTrustWithoutCorpusScan(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	checkpoint := restoredCorpusSignalCheckpoint(now.Add(-time.Minute))
	checkpoint.Citations = []hostrank.Citation{
		{SourceURL: "https://a.example/", TargetURL: "https://b.example/", Confidence: 1},
		{SourceURL: "https://b.example/", TargetURL: "https://a.example/", Confidence: 1},
	}
	checkpoint.Authority = hostrank.AuthorityTable{
		"a.example": {Score: 1, Confidence: 1},
		"b.example": {Score: 1, Confidence: 1},
	}
	repository := &memoryCorpusSignalCheckpoints{checkpoint: checkpoint, found: true}
	corpus := &countedCorpus{}
	trust := &mutableHostTrustPolicy{
		policy: hosttrust.Policy{Blend: 0.5, Domains: []string{"a.example"}},
	}
	holder := hostrank.NewHolder()
	refresh := &corpusSignalRefresh{
		documents: corpus, hostRank: holder, trust: trust, checkpoints: repository,
		readTime: func() time.Time { return now },
	}
	if delay := refresh.restoreCheckpoint(t.Context()); delay != 9*time.Minute {
		t.Fatalf("trusted checkpoint delay = %v", delay)
	}
	if holder.Current().Rank("a.example") <= holder.Current().Rank("b.example") {
		t.Fatalf("recomputed authority = %#v", holder.Current())
	}
	stored, attempts, replacements := repository.snapshot()
	if attempts != 1 || replacements != 1 ||
		stored.CompletedAtUnixMilli != checkpoint.CompletedAtUnixMilli {
		t.Fatalf("trust replacement = %#v, %d, %d", stored, attempts, replacements)
	}
	if !reflect.DeepEqual(stored.TrustDomains, trust.policy.Domains) ||
		stored.TrustBlend != trust.policy.Blend {
		t.Fatalf("stored trust policy = %#v", stored)
	}
	if corpus.scans.Load() != 0 {
		t.Fatal("trust recomputation scanned corpus")
	}
}

func TestCorpusSignalCheckpointPreservesStateAcrossFailures(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	loadFailure := &memoryCorpusSignalCheckpoints{loadError: errors.New("load failed")}
	if delay := (&corpusSignalRefresh{checkpoints: loadFailure}).restoreCheckpoint(
		t.Context(),
	); delay != 0 {
		t.Fatalf("load failure delay = %v", delay)
	}
	missing := &memoryCorpusSignalCheckpoints{}
	if delay := (&corpusSignalRefresh{checkpoints: missing}).restoreCheckpoint(
		t.Context(),
	); delay != 0 {
		t.Fatalf("missing checkpoint delay = %v", delay)
	}
	if delay := (&corpusSignalRefresh{}).restoreCheckpoint(t.Context()); delay != 0 {
		t.Fatalf("nil repository delay = %v", delay)
	}

	canceledRepository := &memoryCorpusSignalCheckpoints{
		checkpoint: restoredCorpusSignalCheckpoint(now.Add(-time.Minute)), found: true,
	}
	canceledRepository.checkpoint.TrustDomains = []string{"old.example"}
	canceledTrust := &mutableHostTrustPolicy{
		policy: hosttrust.Policy{Blend: 0.5, Domains: []string{"source.example"}},
	}
	canceledHolder := hostrank.NewHolder()
	canceledRefresh := &corpusSignalRefresh{
		hostRank: canceledHolder, trust: canceledTrust, checkpoints: canceledRepository,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if delay := canceledRefresh.restoreCheckpoint(ctx); delay != 0 {
		t.Fatalf("canceled trust delay = %v", delay)
	}
	if len(canceledHolder.Current()) != 0 {
		t.Fatalf("canceled trust published = %#v", canceledHolder.Current())
	}

	persistFailure := &memoryCorpusSignalCheckpoints{
		checkpoint:   restoredCorpusSignalCheckpoint(now.Add(-time.Minute)),
		found:        true,
		replaceError: errors.New("persist failed"),
	}
	persistFailure.checkpoint.TrustDomains = []string{"old.example"}
	persistHolder := hostrank.NewHolder()
	persistRefresh := &corpusSignalRefresh{
		hostRank: persistHolder, trust: canceledTrust, checkpoints: persistFailure,
		readTime: func() time.Time { return now },
	}
	if delay := persistRefresh.restoreCheckpoint(t.Context()); delay != 9*time.Minute {
		t.Fatalf("persistence failure delay = %v", delay)
	}
	if persistHolder.Current().Rank("source.example") <= 0.2 {
		t.Fatalf("recomputed checkpoint was not published: %#v", persistHolder.Current())
	}
	_, attempts, replacements := persistFailure.snapshot()
	if attempts != 1 || replacements != 0 {
		t.Fatalf("persistence failure writes = %d, %d", attempts, replacements)
	}
}

func TestCorpusSignalRefreshPersistsOnlySuccessfulCompleteScan(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repository := &memoryCorpusSignalCheckpoints{}
	corpus := &countedCorpus{documents: corpusSignalDocuments()}
	trust := &mutableHostTrustPolicy{changes: make(chan struct{}, 1)}
	holder := hostrank.NewHolder()
	hostLinkSnapshot := hostlinks.NewSnapshotHolder()
	refresh := &corpusSignalRefresh{
		documents: corpus, hostRank: holder, spell: spellcheck.NewHolder(),
		wordForms: wordforms.NewHolder(), includeWordForms: true, trust: trust,
		hostLinks:   hostLinkSnapshot,
		checkpoints: repository, readTime: func() time.Time { return now },
	}
	refresh.scanAndPublish(t.Context())
	stored, attempts, replacements := repository.snapshot()
	if attempts != 1 || replacements != 1 || stored.CompletedAtUnixMilli != now.UnixMilli() ||
		len(stored.Citations) != 2 || !stored.WordFormsReady || stored.Spelling["golang"] != 4 ||
		!stored.HostLinksReady || stored.HostLinks.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("successful checkpoint = %#v, %d, %d", stored, attempts, replacements)
	}

	corpus.err = errors.New("scan failed")
	refresh.scanAndPublish(t.Context())
	if _, currentAttempts, currentReplacements := repository.snapshot(); currentAttempts != attempts ||
		currentReplacements != replacements {
		t.Fatalf("failed scan writes = %d, %d", currentAttempts, currentReplacements)
	}
	corpus.err = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	refresh.scanAndPublish(ctx)
	if _, currentAttempts, currentReplacements := repository.snapshot(); currentAttempts != attempts ||
		currentReplacements != replacements {
		t.Fatalf("canceled scan writes = %d, %d", currentAttempts, currentReplacements)
	}

	trust.policy = hosttrust.Policy{Blend: 0.5, Domains: []string{"source.example"}}
	refresh.publishAuthority(t.Context())
	trusted, currentAttempts, currentReplacements := repository.snapshot()
	if currentAttempts != attempts+1 || currentReplacements != replacements+1 ||
		trusted.CompletedAtUnixMilli != stored.CompletedAtUnixMilli ||
		!reflect.DeepEqual(trusted.HostLinks, stored.HostLinks) {
		t.Fatalf("trust checkpoint = %#v, %d, %d", trusted, currentAttempts, currentReplacements)
	}

	failedRepository := &memoryCorpusSignalCheckpoints{replaceError: errors.New("write failed")}
	failedHolder := hostrank.NewHolder()
	failedRefresh := &corpusSignalRefresh{
		documents: &countedCorpus{documents: corpusSignalDocuments()},
		hostRank:  failedHolder, checkpoints: failedRepository,
		readTime: func() time.Time { return now },
	}
	failedRefresh.scanAndPublish(t.Context())
	if failedHolder.Current().Rank("target.example") != 1 {
		t.Fatalf("persistence failure suppressed signals: %#v", failedHolder.Current())
	}
	if _, failedAttempts, failedReplacements := failedRepository.snapshot(); failedAttempts != 1 ||
		failedReplacements != 0 {
		t.Fatalf("failed checkpoint writes = %d, %d", failedAttempts, failedReplacements)
	}

	lateCancelRepository := &memoryCorpusSignalCheckpoints{}
	lateContext, lateCancel := context.WithCancel(t.Context())
	lateCancelRefresh := &corpusSignalRefresh{
		documents:   &countedCorpus{documents: corpusSignalDocuments()},
		checkpoints: lateCancelRepository,
		readTime: func() time.Time {
			lateCancel()

			return now
		},
	}
	lateCancelRefresh.scanAndPublish(lateContext)
	if lateCancelRefresh.citationsReady {
		t.Fatal("late cancellation published signals")
	}
	if _, lateAttempts, lateReplacements := lateCancelRepository.snapshot(); lateAttempts != 0 ||
		lateReplacements != 0 {
		t.Fatalf("late cancellation writes = %d, %d", lateAttempts, lateReplacements)
	}
}

func TestCorpusSignalCheckpointKeepsAuthorityAndTrustPolicyConsistent(t *testing.T) {
	repository := &memoryCorpusSignalCheckpoints{}
	trust := &changingCorpusSignalTrustPolicy{
		first: hosttrust.Policy{Blend: 0.5, Domains: []string{"source.example"}},
		next:  hosttrust.Policy{Blend: 0.5, Domains: []string{"other.example"}},
	}
	refresh := &corpusSignalRefresh{
		documents:   &countedCorpus{documents: corpusSignalDocuments()},
		hostRank:    hostrank.NewHolder(),
		trust:       trust,
		checkpoints: repository,
		readTime: func() time.Time {
			return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
		},
	}
	refresh.scanAndPublish(t.Context())
	stored, _, replacements := repository.snapshot()
	if replacements != 1 || trust.calls.Load() != 1 {
		t.Fatalf("checkpoint replacements = %d, trust reads = %d", replacements, trust.calls.Load())
	}
	if !reflect.DeepEqual(stored.TrustDomains, trust.first.Domains) ||
		stored.TrustBlend != trust.first.Blend {
		t.Fatalf("stored trust policy = %#v", stored)
	}
	wantAuthority, err := hostrank.ComputeDomainAuthority(
		t.Context(),
		stored.Citations,
		hostrank.DomainOptions{
			TrustedDomains: trust.first.Domains,
			TrustBlend:     trust.first.Blend,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.Authority, wantAuthority) {
		t.Fatalf("stored authority = %#v, want %#v", stored.Authority, wantAuthority)
	}
}

func TestCorpusSignalCheckpointStartupFallbacks(t *testing.T) {
	existing := &corpusSignalRefresh{}
	if got := corpusSignalRefreshForNode(t.Context(), node{corpusPass: existing}); got != existing {
		t.Fatal("assembled corpus refresh was replaced")
	}
	fallback := corpusSignalRefreshForNode(t.Context(), node{})
	if fallback == nil || fallback.readTime == nil {
		t.Fatalf("fallback corpus refresh = %#v", fallback)
	}
	runCorpusSignalRefreshLoop(t.Context(), nil)

	corpus := &countedCorpus{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runCorpusSignalRefreshLoop(ctx, &corpusSignalRefresh{
		documents: corpus, initialRefreshDelay: time.Minute,
	})
	if corpus.scans.Load() != 0 {
		t.Fatal("canceled initial delay scanned corpus")
	}
}

func TestRemainingCorpusSignalRefreshDelay(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		completedAt time.Time
		want        time.Duration
	}{
		{completedAt: now, want: defaultCorpusSignalRefreshInterval},
		{completedAt: now.Add(-time.Minute), want: 9 * time.Minute},
		{completedAt: now.Add(-defaultCorpusSignalRefreshInterval), want: 0},
		{completedAt: now.Add(-time.Hour), want: 0},
		{completedAt: now.Add(time.Millisecond), want: 0},
	}
	for _, testCase := range cases {
		if got := remainingCorpusSignalRefreshDelay(
			now,
			testCase.completedAt.UnixMilli(),
		); got != testCase.want {
			t.Errorf("delay for %v = %v, want %v", testCase.completedAt, got, testCase.want)
		}
	}
}

var _ documentstore.StoredDocuments = (*countedCorpus)(nil)
