package yagonode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

const importedRecrawlRetryInterval = time.Hour

type documentImportService struct {
	documents       documentstore.DocumentDirectory
	ingest          documentImportSubmitter
	frontier        importedScheduleWriter
	profile         yagocrawlcontract.CrawlProfile
	profileMu       sync.Mutex
	profileRecorded bool
}

type documentImportSubmitter interface {
	SubmitImport(
		context.Context,
		yagocrawlcontract.IngestBatch,
		yagocrawlcontract.CrawlProfile,
	) error
}

type concurrentDocumentImportSubmitter interface {
	MaximumConcurrentImports() int
}

type indexedImportRecord struct {
	index       int
	record      adminui.IndexImportRecord
	destination importDestination
}

type importDestination struct {
	known    bool
	found    bool
	document documentstore.Document
}

type importBatchExecution struct {
	ctx      context.Context
	cancel   context.CancelFunc
	results  []adminui.IndexImportResult
	firstErr error
	failOnce sync.Once
}

type importedScheduleWriter interface {
	RecordProfile(
		context.Context,
		yagocrawlcontract.CrawlProfile,
		yagocrawlcontract.CrawlOrderPriority,
	) error
	ScheduleAtIfAbsent(context.Context, string, string, time.Time, time.Duration) (bool, error)
}

type importedScheduleBatchWriter interface {
	ScheduleRecrawlsAtIfAbsent(
		context.Context,
		string,
		[]recrawlfrontier.RecrawlSchedule,
		time.Duration,
	) error
}

func newDocumentImportService(
	documents documentstore.DocumentDirectory,
	ingest *crawlbroker.IngestReceiver,
	frontier *recrawlfrontier.Frontier,
) *documentImportService {
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "Document migration recrawl",
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: 1,
		AllowQueryURLs:  true,
		Formats:         yagocrawlcontract.DefaultFormatToggles(),
	})
	return &documentImportService{
		documents: documents,
		ingest:    ingest,
		frontier:  frontier,
		profile:   profile,
	}
}

func (s *documentImportService) ImportDocuments(
	ctx context.Context,
	records []adminui.IndexImportRecord,
) ([]adminui.IndexImportResult, error) {
	if err := s.ensureProfile(ctx); err != nil {
		return nil, fmt.Errorf("register document migration profile: %w", err)
	}
	if len(records) == 0 {
		return []adminui.IndexImportResult{}, nil
	}
	destinations, err := s.importDestinationSnapshots(ctx, records)
	if err != nil {
		return nil, fmt.Errorf("read import destination batch: %w", err)
	}
	results, err := s.processImportRecords(ctx, records, destinations)
	if err != nil {
		return nil, err
	}
	if err := s.scheduleImportBatch(ctx, records, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *documentImportService) processImportRecords(
	ctx context.Context,
	records []adminui.IndexImportRecord,
	destinations map[string]importDestination,
) ([]adminui.IndexImportResult, error) {
	workerCount := documentImportWorkerCount(s.ingest, len(records))
	queues := make([]chan indexedImportRecord, workerCount)
	queueCapacity := (len(records) + workerCount - 1) / workerCount
	for index := range queues {
		queues[index] = make(chan indexedImportRecord, queueCapacity)
	}
	execution := importBatchExecution{
		results: make([]adminui.IndexImportResult, len(records)),
	}
	execution.ctx, execution.cancel = context.WithCancel(ctx)
	defer execution.cancel()
	var workers sync.WaitGroup
	s.startImportWorkers(queues, &workers, &execution)
	enqueueImportRecords(execution.ctx, records, destinations, queues)
	workers.Wait()
	if execution.firstErr != nil {
		return nil, execution.firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("import document batch: %w", err)
	}
	return execution.results, nil
}

func (s *documentImportService) startImportWorkers(
	queues []chan indexedImportRecord,
	workers *sync.WaitGroup,
	execution *importBatchExecution,
) {
	for _, queue := range queues {
		workers.Add(1)
		go func(jobs <-chan indexedImportRecord) {
			defer workers.Done()
			s.processImportWorker(jobs, execution)
		}(queue)
	}
}

func (s *documentImportService) processImportWorker(
	jobs <-chan indexedImportRecord,
	execution *importBatchExecution,
) {
	resolved := make(map[string]importDestination, len(jobs))
	for job := range jobs {
		destination, found := resolved[job.record.Document.NormalizedURL]
		if !found {
			destination = job.destination
		}
		result, destination, err := s.importDocument(execution.ctx, job.record, destination)
		if err != nil {
			execution.failOnce.Do(func() {
				execution.firstErr = err
				execution.cancel()
			})
			return
		}
		execution.results[job.index] = result
		resolved[job.record.Document.NormalizedURL] = destination
	}
}

func enqueueImportRecords(
	ctx context.Context,
	records []adminui.IndexImportRecord,
	destinations map[string]importDestination,
	queues []chan indexedImportRecord,
) {
	for index, record := range records {
		queue := queues[importWorkerForURL(record.Document.NormalizedURL, len(queues))]
		select {
		case queue <- indexedImportRecord{
			index:       index,
			record:      record,
			destination: destinations[record.Document.NormalizedURL],
		}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	for _, queue := range queues {
		close(queue)
	}
}

func (s *documentImportService) importDestinationSnapshots(
	ctx context.Context,
	records []adminui.IndexImportRecord,
) (map[string]importDestination, error) {
	documents, ok := s.documents.(documentstore.DocumentSetDirectory)
	if !ok {
		return nil, nil
	}
	urls := make([]string, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		url := record.Document.NormalizedURL
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	stored, found, err := documents.Documents(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("read destination document set: %w", err)
	}
	if len(stored) != len(urls) || len(found) != len(urls) {
		return nil, fmt.Errorf(
			"document store returned %d documents and %d presence flags for %d URLs",
			len(stored),
			len(found),
			len(urls),
		)
	}
	destinations := make(map[string]importDestination, len(urls))
	for index, url := range urls {
		destinations[url] = importDestination{
			known:    true,
			found:    found[index],
			document: stored[index],
		}
	}

	return destinations, nil
}

func documentImportWorkerCount(submitter documentImportSubmitter, recordCount int) int {
	maximum := 1
	if concurrent, ok := submitter.(concurrentDocumentImportSubmitter); ok {
		maximum = concurrent.MaximumConcurrentImports()
	}
	if maximum < 1 {
		maximum = 1
	}
	if maximum > yagocrawlcontract.MaximumIngestBatchRecords {
		maximum = yagocrawlcontract.MaximumIngestBatchRecords
	}
	return min(recordCount, maximum)
}

func importWorkerForURL(rawURL string, workerCount int) int {
	if workerCount <= 1 || workerCount > yagocrawlcontract.MaximumIngestBatchRecords {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(rawURL))
	return int(hash.Sum32() % uint32(workerCount))
}

func (s *documentImportService) importDocument(
	ctx context.Context,
	record adminui.IndexImportRecord,
	destination importDestination,
) (adminui.IndexImportResult, importDestination, error) {
	if err := ctx.Err(); err != nil {
		return adminui.IndexImportResult{}, destination, fmt.Errorf(
			"import document batch: %w",
			err,
		)
	}
	if !destination.known {
		existing, found, err := s.existingDestination(ctx, record.Document)
		if err != nil {
			return adminui.IndexImportResult{}, destination, fmt.Errorf(
				"check destination document: %w",
				err,
			)
		}
		destination = importDestination{known: true, found: found, document: existing}
	}
	if destination.found {
		return importResultForExisting(record, destination.document), destination, nil
	}
	batch := yagocrawlcontract.IngestBatch{
		SourceURL:     record.Document.NormalizedURL,
		ProfileHandle: s.profile.Handle,
		ObservationID: importObservationID(record.ID),
		ObservedAt:    time.Now().UTC(),
		Document:      record.Document,
	}
	if err := s.ingest.SubmitImport(ctx, batch, s.profile); err != nil {
		return adminui.IndexImportResult{}, destination, fmt.Errorf(
			"ingest imported document: %w",
			err,
		)
	}
	destination.found = true
	destination.document = documentstore.Document{
		NormalizedURL: record.Document.NormalizedURL,
		ContentHash:   record.Document.ContentHash,
	}

	return adminui.IndexImportResult{ID: record.ID, Status: "imported"}, destination, nil
}

func importResultForExisting(
	record adminui.IndexImportRecord,
	existing documentstore.Document,
) adminui.IndexImportResult {
	status := "already-present"
	if existing.ContentHash == "" || existing.ContentHash != record.Document.ContentHash {
		status = "conflict"
	}

	return adminui.IndexImportResult{ID: record.ID, Status: status}
}

func (s *documentImportService) scheduleImportBatch(
	ctx context.Context,
	records []adminui.IndexImportRecord,
	results []adminui.IndexImportResult,
) error {
	schedules := make([]recrawlfrontier.RecrawlSchedule, 0, len(records))
	for index, record := range records {
		if record.NextVisitAt == nil ||
			(results[index].Status != "imported" && results[index].Status != "already-present") {
			continue
		}
		schedules = append(schedules, recrawlfrontier.RecrawlSchedule{
			URL:   record.Document.NormalizedURL,
			DueAt: *record.NextVisitAt,
		})
	}
	if len(schedules) == 0 {
		return nil
	}
	if batchWriter, ok := s.frontier.(importedScheduleBatchWriter); ok {
		if err := batchWriter.ScheduleRecrawlsAtIfAbsent(
			ctx,
			s.profile.Handle,
			schedules,
			importedRecrawlRetryInterval,
		); err != nil {
			return fmt.Errorf("schedule imported recrawls: %w", err)
		}

		return nil
	}
	for index, record := range records {
		if record.NextVisitAt == nil ||
			(results[index].Status != "imported" && results[index].Status != "already-present") {
			continue
		}
		if err := s.scheduleImport(ctx, record); err != nil {
			return err
		}
	}

	return nil
}

func (s *documentImportService) ensureProfile(ctx context.Context) error {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	if s.profileRecorded {
		return nil
	}
	if err := s.frontier.RecordProfile(
		ctx,
		s.profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		return fmt.Errorf("record document migration profile: %w", err)
	}
	s.profileRecorded = true
	return nil
}

func (s *documentImportService) existingDestination(
	ctx context.Context,
	document yagocrawlcontract.DocumentIngest,
) (documentstore.Document, bool, error) {
	existing, found, err := s.documents.Document(ctx, document.NormalizedURL)
	if err != nil || !found {
		if err != nil {
			return documentstore.Document{}, false, fmt.Errorf(
				"read existing normalized URL: %w",
				err,
			)
		}
		return documentstore.Document{}, found, nil
	}
	return existing, true, nil
}

func (s *documentImportService) scheduleImport(
	ctx context.Context,
	record adminui.IndexImportRecord,
) error {
	if _, err := s.frontier.ScheduleAtIfAbsent(
		ctx,
		record.Document.NormalizedURL,
		s.profile.Handle,
		*record.NextVisitAt,
		importedRecrawlRetryInterval,
	); err != nil {
		return fmt.Errorf("store imported recrawl schedule: %w", err)
	}
	return nil
}

func importObservationID(recordID string) string {
	sum := sha256.Sum256([]byte(recordID))
	return "migration-" + hex.EncodeToString(sum[:])
}

var _ adminui.IndexImporter = (*documentImportService)(nil)
