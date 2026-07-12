package searchindex

import (
	"context"
	"log/slog"
	"time"

	"github.com/blevesearch/bleve/v2"
)

const (
	bleveLexicalWarmupBudget            = 15 * time.Second
	bleveLexicalWarmupIncompleteMessage = "bleve lexical warmup incomplete"
)

type bleveLexicalWarmupFailure struct {
	shard     int
	field     string
	operation string
	err       error
}

type bleveLexicalWarmupReport struct {
	attempted    int
	failures     int
	firstFailure bleveLexicalWarmupFailure
	interruption error
}

func (r *bleveLexicalWarmupReport) merge(other bleveLexicalWarmupReport) {
	r.attempted += other.attempted
	r.failures += other.failures
	if r.firstFailure.err == nil {
		r.firstFailure = other.firstFailure
	}
	r.interruption = other.interruption
}

func (r *bleveLexicalWarmupReport) recordFailure(
	shard int,
	field string,
	operation string,
	err error,
) {
	r.failures++
	if r.firstFailure.err == nil {
		r.firstFailure = bleveLexicalWarmupFailure{
			shard: shard, field: field, operation: operation, err: err,
		}
	}
}

func (b *BleveDiskIndex) warm(ctx context.Context) {
	warmupContext, cancel := context.WithTimeout(ctx, bleveLexicalWarmupBudget)
	defer cancel()

	report := bleveLexicalWarmupReport{}
	if err := warmupContext.Err(); err != nil {
		report.interruption = err
	} else {
		report.attempted++
		request := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
		request.Size = 1
		if _, err := b.alias.SearchInContext(warmupContext, request); err != nil {
			report.recordFailure(-1, "_id", "match-all", err)
		}
	}
	if report.interruption == nil {
		report.merge(warmBleveLexicalDictionaries(warmupContext, b.shards))
	}
	if report.failures == 0 && report.interruption == nil {
		return
	}
	attributes := []any{
		slog.Int("attempted", report.attempted),
		slog.Int("failures", report.failures),
	}
	if report.firstFailure.err != nil {
		attributes = append(
			attributes,
			slog.Int("shard", report.firstFailure.shard),
			slog.String("field", report.firstFailure.field),
			slog.String("operation", report.firstFailure.operation),
			slog.Any("error", report.firstFailure.err),
		)
	}
	if report.interruption != nil {
		attributes = append(attributes, slog.Any("interruption", report.interruption))
	}
	slog.WarnContext(ctx, bleveLexicalWarmupIncompleteMessage, attributes...)
}

func warmBleveLexicalDictionaries(
	ctx context.Context,
	shards []bleve.Index,
) bleveLexicalWarmupReport {
	report := bleveLexicalWarmupReport{}
	fields := append([]string{documentAnalyzerField}, searchIndexedFields()...)
	for shardNumber, shard := range shards {
		for _, field := range fields {
			if err := ctx.Err(); err != nil {
				report.interruption = err

				return report
			}
			report.attempted++
			dictionary, err := shard.FieldDict(field)
			if err != nil {
				report.recordFailure(shardNumber, field, "open", err)

				continue
			}
			_ = dictionary.Cardinality()
			if err := dictionary.Close(); err != nil {
				report.recordFailure(shardNumber, field, "close", err)
			}
		}
	}

	return report
}
