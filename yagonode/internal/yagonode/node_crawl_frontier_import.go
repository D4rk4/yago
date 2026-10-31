package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

type crawlOrderImportService struct {
	documents documentstore.DocumentSetPresence
	orders    crawldispatch.CrawlOrderQueue
	profiles  interface {
		RecordProfile(
			context.Context,
			yagocrawlcontract.CrawlProfile,
			yagocrawlcontract.CrawlOrderPriority,
		) error
	}
}

func newCrawlOrderImportService(
	documents documentstore.DocumentDirectory,
	orders crawldispatch.CrawlOrderQueue,
	profiles *recrawlfrontier.Frontier,
) *crawlOrderImportService {
	presence, _ := documents.(documentstore.DocumentSetPresence)
	return &crawlOrderImportService{
		documents: presence,
		orders:    orders,
		profiles:  profiles,
	}
}

func (s *crawlOrderImportService) ImportCrawlOrder(
	ctx context.Context,
	batch yagocrawlcontract.CrawlOrderImportBatch,
) (adminui.CrawlOrderImportResult, error) {
	if err := batch.Validate(); err != nil {
		return adminui.CrawlOrderImportResult{}, fmt.Errorf("validate imported frontier: %w", err)
	}
	if s.documents == nil {
		return adminui.CrawlOrderImportResult{}, fmt.Errorf("document presence lookup unavailable")
	}
	urls := make([]string, len(batch.Requests))
	for index, request := range batch.Requests {
		urls[index] = request.URL
	}
	present, err := s.documents.DocumentsExist(ctx, urls)
	if err != nil {
		return adminui.CrawlOrderImportResult{}, fmt.Errorf("check imported frontier URLs: %w", err)
	}
	if len(present) != len(batch.Requests) {
		return adminui.CrawlOrderImportResult{}, fmt.Errorf(
			"document presence lookup returned an incomplete result",
		)
	}
	requests := make([]yagocrawlcontract.CrawlRequest, 0, len(batch.Requests))
	result := adminui.CrawlOrderImportResult{}
	for index, request := range batch.Requests {
		if present[index] {
			result.AlreadyIndexed++
			continue
		}
		requests = append(requests, request)
	}
	if len(requests) == 0 {
		return result, nil
	}
	if err := s.profiles.RecordProfile(
		ctx,
		batch.Profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		return adminui.CrawlOrderImportResult{}, fmt.Errorf(
			"register imported crawl profile: %w",
			err,
		)
	}
	order := yagocrawlcontract.CrawlOrder{
		Provenance: mintProvenance(),
		Priority:   yagocrawlcontract.CrawlOrderPriorityNormal,
		Profile:    batch.Profile,
		Requests:   requests,
	}
	duplicate, err := s.orders.PublishOnce(ctx, batch.IdempotencyKey(), order)
	if err != nil {
		return adminui.CrawlOrderImportResult{}, fmt.Errorf(
			"persist imported crawl frontier: %w",
			err,
		)
	}
	result.DuplicateBatch = duplicate
	if !duplicate {
		result.EnqueuedRequests = len(requests)
	}
	return result, nil
}

var (
	_ adminui.CrawlOrderImporter = (*crawlOrderImportService)(nil)
	_ adminui.CrawlOrderImporter = (*crawlRuntime)(nil)
)
