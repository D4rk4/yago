package rwibackfill

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/stopwords"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	stateIdle                   = "idle"
	stateRunning                = "running"
	statePauseRequested         = "pause_requested"
	statePaused                 = "paused"
	stateComplete               = "complete"
	stateFailed                 = "failed"
	checkpointInterval          = 128
	postingBatchLimit           = yagocrawlcontract.MaximumIngestPostings
	messageCheckpointLoadFailed = "rwi backfill checkpoint load failed"
	messageCheckpointSaveFailed = "rwi backfill checkpoint save failed"
	messageRunFailed            = "rwi backfill run failed"
)

var errBackfillPaused = errors.New("rwi backfill paused")

type documentScanner interface {
	ScanStoredDocumentPagesFrom(
		context.Context,
		string,
		func(*documentstore.Document, string, bool) (bool, error),
	) error
}

type Status struct {
	State               string    `json:"state"`
	ScannedDocuments    int64     `json:"scannedDocuments"`
	IndexedDocuments    int64     `json:"indexedDocuments"`
	SkippedDocuments    int64     `json:"skippedDocuments"`
	PostingRows         int64     `json:"postingRows"`
	EstimatedBytes      int64     `json:"estimatedBytes"`
	MaximumDocumentsRun int64     `json:"maximumDocumentsRun"`
	DryRun              bool      `json:"dryRun"`
	LastError           string    `json:"lastError,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type batch struct {
	metadata       []yagomodel.URIMetadataRow
	postings       []yagomodel.RWIPosting
	scanned        int64
	indexed        int64
	skipped        int64
	postingRows    int64
	estimatedBytes int64
	cursor         string
}

type scanProgress struct {
	checkpoint       *checkpoint
	pending          *batch
	processedThisRun *int64
}

type scanCandidate struct {
	document *documentstore.Document
	cursor   string
	done     bool
}

type Backfill struct {
	mu         sync.Mutex
	wake       chan struct{}
	pause      atomic.Bool
	repository *checkpointRepository
	documents  documentScanner
	urls       urlmeta.URLReceiver
	postings   rwi.PostingReceiver
	clock      func() time.Time
}

func Open(
	storage *vault.Vault,
	documents documentScanner,
	urls urlmeta.URLReceiver,
	postings rwi.PostingReceiver,
) (*Backfill, error) {
	if storage == nil || documents == nil || urls == nil || postings == nil {
		return nil, errors.New("rwi backfill dependencies are required")
	}
	repository, err := openCheckpointRepository(storage)
	if err != nil {
		return nil, err
	}
	return &Backfill{
		wake:       make(chan struct{}, 1),
		repository: repository,
		documents:  documents,
		urls:       urls,
		postings:   postings,
		clock:      time.Now,
	}, nil
}

func (b *Backfill) Start(ctx context.Context, maximumDocuments int64, dryRun bool) (Status, error) {
	if maximumDocuments < 0 || maximumDocuments > 10_000_000 {
		return Status{}, errors.New("maximum documents is outside supported bounds")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, found, err := b.repository.load(ctx)
	if err != nil {
		return Status{}, err
	}
	if found && current.State != stateComplete {
		return statusFrom(
				current,
			), errors.New(
				"an rwi backfill checkpoint already exists; resume or inspect it",
			)
	}
	current = checkpoint{
		Format:              checkpointFormat,
		State:               stateRunning,
		MaximumDocumentsRun: maximumDocuments,
		DryRun:              dryRun,
		UpdatedAtUnixMilli:  b.clock().UTC().UnixMilli(),
	}
	if err := b.repository.save(ctx, current); err != nil {
		return Status{}, err
	}
	b.pause.Store(false)
	b.signal()
	return statusFrom(current), nil
}

func (b *Backfill) Pause(ctx context.Context) (Status, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, found, err := b.repository.load(ctx)
	if err != nil {
		return Status{}, err
	}
	if !found {
		return Status{State: stateIdle}, nil
	}
	if current.State != stateRunning {
		return statusFrom(current), nil
	}
	current.State = statePauseRequested
	current.UpdatedAtUnixMilli = b.clock().UTC().UnixMilli()
	if err := b.repository.save(ctx, current); err != nil {
		return Status{}, err
	}
	b.pause.Store(true)
	b.signal()
	return statusFrom(current), nil
}

func (b *Backfill) Resume(
	ctx context.Context,
	maximumDocuments int64,
	dryRun bool,
) (Status, error) {
	if maximumDocuments < 0 || maximumDocuments > 10_000_000 {
		return Status{}, errors.New("maximum documents is outside supported bounds")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, found, err := b.repository.load(ctx)
	if err != nil {
		return Status{}, err
	}
	if !found || (current.State != statePaused && current.State != stateFailed) {
		return statusFrom(current), errors.New("rwi backfill is not resumable in its current state")
	}
	current.State = stateRunning
	current.MaximumDocumentsRun = maximumDocuments
	current.DryRun = dryRun
	current.LastError = ""
	current.UpdatedAtUnixMilli = b.clock().UTC().UnixMilli()
	if err := b.repository.save(ctx, current); err != nil {
		return Status{}, err
	}
	b.pause.Store(false)
	b.signal()
	return statusFrom(current), nil
}

func (b *Backfill) Status(ctx context.Context) (Status, error) {
	current, found, err := b.repository.load(ctx)
	if err != nil {
		return Status{}, err
	}
	if !found {
		return Status{State: stateIdle}, nil
	}
	return statusFrom(current), nil
}

func (b *Backfill) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	b.runPersistedCheckpoint(ctx)
	for b.waitForSignal(ctx) {
		b.runPersistedCheckpoint(ctx)
	}
}

func (b *Backfill) runPersistedCheckpoint(ctx context.Context) {
	current, found, err := b.repository.load(ctx)
	if err != nil {
		slog.ErrorContext(ctx, messageCheckpointLoadFailed, slog.Any("error", err))
		return
	}
	if !found {
		return
	}
	switch current.State {
	case statePauseRequested:
		b.persistRequestedPause(ctx, current)
	case stateRunning:
		b.execute(ctx, current)
	}
}

func (b *Backfill) persistRequestedPause(ctx context.Context, current checkpoint) {
	if err := b.persistState(ctx, current, statePaused, ""); err != nil {
		slog.ErrorContext(ctx, messageCheckpointSaveFailed, slog.Any("error", err))
	}
}

func (b *Backfill) waitForSignal(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-b.wake:
		return ctx.Err() == nil
	}
}

func (b *Backfill) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *Backfill) execute(ctx context.Context, current checkpoint) {
	processedThisRun := int64(0)
	for {
		yielded, stopped, err := b.scanBatch(ctx, &current, &processedThisRun)
		if err != nil {
			b.handleExecutionError(ctx, current, err)
			return
		}
		if stopped {
			return
		}
		if !yielded {
			b.handleExecutionError(
				ctx,
				current,
				errors.New("stored document scanner stopped before its boundary"),
			)
			return
		}
	}
}

func (b *Backfill) scanBatch(
	ctx context.Context,
	current *checkpoint,
	processedThisRun *int64,
) (bool, bool, error) {
	var pending batch
	progress := scanProgress{
		checkpoint: current, pending: &pending, processedThisRun: processedThisRun,
	}
	yielded := false
	stopped := false
	err := b.documents.ScanStoredDocumentPagesFrom(
		ctx,
		current.Cursor,
		func(document *documentstore.Document, cursor string, done bool) (bool, error) {
			keepGoing, actionErr := b.visitBackfillDocument(ctx, progress, scanCandidate{
				document: document, cursor: cursor, done: done,
			})
			if actionErr != nil {
				return false, actionErr
			}
			yielded = !keepGoing && !done && !b.pause.Load() &&
				!runDocumentLimitReached(*current, *processedThisRun)
			stopped = !keepGoing && !yielded
			return keepGoing, nil
		},
	)
	if err != nil {
		return yielded, stopped, fmt.Errorf("scan stored documents for rwi backfill: %w", err)
	}
	return yielded, stopped, nil
}

func (b *Backfill) visitBackfillDocument(
	ctx context.Context,
	progress scanProgress,
	candidate scanCandidate,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("backfill context: %w", err)
	}
	if b.pause.Load() {
		return false, b.pauseExecution(ctx, *progress.checkpoint, progress.pending)
	}
	if candidate.document != nil {
		if err := b.appendDocument(
			ctx,
			progress,
			*candidate.document,
		); err != nil {
			return false, err
		}
	}
	progress.pending.cursor = candidate.cursor
	return b.finishBackfillDocument(ctx, progress, candidate.done)
}

func (b *Backfill) appendDocument(
	ctx context.Context,
	progress scanProgress,
	document documentstore.Document,
) error {
	artifacts, ok := buildArtifacts(document)
	if ok && len(progress.pending.postings) > 0 &&
		len(progress.pending.postings)+len(artifacts.Postings) > postingBatchLimit {
		if err := b.flush(ctx, progress.checkpoint, progress.pending); err != nil {
			return err
		}
	}
	*progress.processedThisRun++
	progress.pending.scanned++
	if !ok {
		progress.pending.skipped++
		return nil
	}
	progress.pending.metadata = append(progress.pending.metadata, artifacts.Metadata)
	progress.pending.postings = append(progress.pending.postings, artifacts.Postings...)
	progress.pending.indexed++
	progress.pending.postingRows += int64(len(artifacts.Postings))
	progress.pending.estimatedBytes += int64(len(artifacts.Metadata.String()))
	for _, posting := range artifacts.Postings {
		progress.pending.estimatedBytes += int64(len(posting.String()))
	}
	return nil
}

func (b *Backfill) finishBackfillDocument(
	ctx context.Context,
	progress scanProgress,
	done bool,
) (bool, error) {
	limitReached := runDocumentLimitReached(*progress.checkpoint, *progress.processedThisRun)
	mustFlush := limitReached || done || progress.pending.scanned >= checkpointInterval ||
		len(progress.pending.postings) >= postingBatchLimit
	if mustFlush {
		if err := b.flush(ctx, progress.checkpoint, progress.pending); err != nil {
			return false, err
		}
	}
	if done {
		return false, b.persistState(ctx, *progress.checkpoint, stateComplete, "")
	}
	if limitReached || b.pause.Load() {
		return false, b.persistState(ctx, *progress.checkpoint, statePaused, "")
	}
	return !mustFlush, nil
}

func (b *Backfill) pauseExecution(ctx context.Context, current checkpoint, pending *batch) error {
	if err := b.flush(ctx, &current, pending); err != nil {
		return err
	}
	return b.persistState(ctx, current, statePaused, "")
}

func runDocumentLimitReached(current checkpoint, processedThisRun int64) bool {
	return current.MaximumDocumentsRun > 0 && processedThisRun >= current.MaximumDocumentsRun
}

func (b *Backfill) handleExecutionError(ctx context.Context, current checkpoint, err error) {
	if ctx.Err() != nil {
		return
	}
	state := stateFailed
	reason := "backfill scan or write failed"
	if errors.Is(err, errBackfillPaused) {
		state = statePaused
		reason = ""
	}
	if persistErr := b.persistState(ctx, current, state, reason); persistErr != nil {
		slog.ErrorContext(ctx, messageCheckpointSaveFailed, slog.Any("error", persistErr))
	}
	if state == stateFailed {
		slog.ErrorContext(ctx, messageRunFailed, slog.Any("error", err))
	}
}

func (b *Backfill) flush(ctx context.Context, current *checkpoint, pending *batch) error {
	if pending.cursor == "" {
		return nil
	}
	if !current.DryRun {
		if len(pending.metadata) > 0 {
			if err := b.receiveURLMetadata(ctx, pending.metadata); err != nil {
				return err
			}
		}
		if len(pending.postings) > 0 {
			if err := b.receivePostings(ctx, pending.postings); err != nil {
				return err
			}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	latest, found, err := b.repository.load(ctx)
	if err != nil {
		return err
	}
	if found && latest.State == statePauseRequested {
		current.State = statePaused
	}
	current.Cursor = pending.cursor
	current.ScannedDocuments += pending.scanned
	current.IndexedDocuments += pending.indexed
	current.SkippedDocuments += pending.skipped
	current.PostingRows += pending.postingRows
	current.EstimatedBytes += pending.estimatedBytes
	current.UpdatedAtUnixMilli = b.clock().UTC().UnixMilli()
	if err := b.repository.save(ctx, *current); err != nil {
		return err
	}
	*pending = batch{}
	if current.State == statePaused {
		b.pause.Store(true)
	}
	return nil
}

func (b *Backfill) receiveURLMetadata(
	ctx context.Context,
	rows []yagomodel.URIMetadataRow,
) error {
	for attempt := 0; ; attempt++ {
		receipt, err := b.urls.Receive(ctx, rows)
		if err != nil {
			return fmt.Errorf("receive backfill url metadata: %w", err)
		}
		if receipt.Busy {
			if err := b.waitForRetry(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		if len(receipt.ErrorURL) == 0 {
			return nil
		}
		return errors.New("URL metadata receiver rejected backfill rows")
	}
}

func (b *Backfill) receivePostings(
	ctx context.Context,
	rows []yagomodel.RWIPosting,
) error {
	for attempt := 0; ; attempt++ {
		receipt, err := b.postings.Receive(ctx, rows)
		if err != nil {
			return fmt.Errorf("receive backfill postings: %w", err)
		}
		if receipt.Busy {
			if err := b.waitForRetry(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		if len(receipt.UnknownURL) == 0 {
			return nil
		}
		return errors.New("RWI receiver rejected backfill URL references")
	}
}

func (b *Backfill) waitForRetry(ctx context.Context, attempt int) error {
	if b.pause.Load() {
		return errBackfillPaused
	}
	delay := 100 * time.Millisecond
	for range min(attempt, 8) {
		delay = min(delay*2, 5*time.Second)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	return b.waitForRetrySignal(ctx, timer.C)
}

func (b *Backfill) waitForRetrySignal(ctx context.Context, timer <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("backfill retry context: %w", ctx.Err())
	case <-b.wake:
		if b.pause.Load() {
			return errBackfillPaused
		}
		return nil
	case <-timer:
		return nil
	}
}

func (b *Backfill) persistState(
	ctx context.Context,
	current checkpoint,
	state, reason string,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	latest, found, err := b.repository.load(ctx)
	if err != nil {
		return err
	}
	if found {
		current = latest
	}
	current.State = state
	current.LastError = reason
	current.UpdatedAtUnixMilli = b.clock().UTC().UnixMilli()
	return b.repository.save(ctx, current)
}

func statusFrom(value checkpoint) Status {
	return Status{
		State:               value.State,
		ScannedDocuments:    value.ScannedDocuments,
		IndexedDocuments:    value.IndexedDocuments,
		SkippedDocuments:    value.SkippedDocuments,
		PostingRows:         value.PostingRows,
		EstimatedBytes:      value.EstimatedBytes,
		MaximumDocumentsRun: value.MaximumDocumentsRun,
		DryRun:              value.DryRun,
		LastError:           value.LastError,
		UpdatedAt:           time.UnixMilli(value.UpdatedAtUnixMilli).UTC(),
	}
}

func buildArtifacts(document documentstore.Document) (yagomodel.RWIPageArtifacts, bool) {
	if !indexableFetchStatus(document.FetchStatus) {
		return yagomodel.RWIPageArtifacts{}, false
	}
	tokens := yagomodel.TokenizeRWIText(
		document.ExtractedText,
		yagocrawlcontract.MaximumDocumentWords,
	)
	titleTokens := yagomodel.TokenizeRWIText(document.Title, 255)
	wordCapacity := min(len(tokens), yagocrawlcontract.MaximumIngestPostings)
	frequencies := make(map[string]int, wordCapacity)
	firstPositions := make(map[string]int, wordCapacity)
	order := make([]string, 0, wordCapacity)
	for position, token := range tokens {
		if stopwords.IsStopword(token) {
			continue
		}
		if _, exists := frequencies[token]; !exists {
			if len(order) == yagocrawlcontract.MaximumIngestPostings {
				continue
			}
			order = append(order, token)
			firstPositions[token] = position
		}
		frequencies[token]++
	}
	words := make([]yagomodel.RWIWord, 0, len(order))
	for _, word := range order {
		words = append(words, yagomodel.RWIWord{
			Word:          word,
			Frequency:     frequencies[word],
			FirstPosition: firstPositions[word],
		})
	}
	localLinks, externalLinks := linkCounts(document.NormalizedURL, document.Outlinks)
	loadedAt := document.FetchedAt
	if loadedAt.IsZero() {
		loadedAt = document.IndexedAt
	}
	artifacts := yagomodel.BuildRWIPageArtifacts(yagomodel.RWIPage{
		URL:               document.NormalizedURL,
		Title:             document.Title,
		Language:          document.Language,
		WordCount:         len(tokens),
		TitleWordCount:    len(titleTokens),
		TextBytes:         len(document.ExtractedText),
		LocalLinkCount:    localLinks,
		ExternalLinkCount: externalLinks,
		Words:             words,
		LoadedAt:          loadedAt,
		ModifiedAt:        document.ModifiedAt,
		FreshUntil:        importedFreshness(document.Metadata["yacy.fresh_until"]),
	}, yagocrawlcontract.MaximumIngestPostings)
	return artifacts, true
}

func indexableFetchStatus(status string) bool {
	if status == "" || status == "fetched" {
		return true
	}
	code, err := strconv.Atoi(status)
	return err == nil && code >= 200 && code < 300
}

func linkCounts(source string, outlinks []string) (int, int) {
	base, err := url.Parse(source)
	if err != nil {
		return 0, 0
	}
	local, external := 0, 0
	for _, raw := range outlinks {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			continue
		}
		if strings.EqualFold(parsed.Host, base.Host) {
			local++
		} else {
			external++
		}
	}
	return local, external
}

func importedFreshness(raw string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
