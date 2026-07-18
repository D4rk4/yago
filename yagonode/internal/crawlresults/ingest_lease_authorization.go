package crawlresults

import (
	"context"
	"log/slog"
)

const msgIngestLeaseLost = "crawl ingest lease lost"

func authorizeIngestDelivery(
	ctx context.Context,
	delivery IngestDelivery,
) (func(), bool) {
	if !validateIngestDelivery(ctx, delivery) {
		return func() {}, false
	}

	return authorizeValidatedIngestDelivery(ctx, delivery)
}

func validateIngestDelivery(ctx context.Context, delivery IngestDelivery) bool {
	if delivery.ValidateMutation == nil {
		return true
	}
	if err := delivery.ValidateMutation(ctx); err != nil {
		rejectIngestAuthorization(ctx, delivery, err)

		return false
	}

	return true
}

func authorizeValidatedIngestDelivery(
	ctx context.Context,
	delivery IngestDelivery,
) (func(), bool) {
	if delivery.BeginMutation == nil {
		return func() {}, true
	}
	release, err := delivery.BeginMutation(ctx)
	if err == nil {
		return release, true
	}
	rejectIngestAuthorization(ctx, delivery, err)

	return func() {}, false
}

func rejectIngestAuthorization(
	ctx context.Context,
	delivery IngestDelivery,
	err error,
) {
	slog.DebugContext(ctx, msgIngestLeaseLost,
		slog.String("sourceUrl", delivery.Batch.SourceURL),
		slog.Any("error", err),
	)
	if delivery.LeaseLost != nil {
		_ = delivery.LeaseLost(ctx)
	} else if delivery.Nak != nil {
		_ = delivery.Nak(ctx)
	}
}

func authorizeIngestGroup(
	ctx context.Context,
	group []IngestDelivery,
) ([]IngestDelivery, []func()) {
	candidates := make([]IngestDelivery, 0, len(group))
	for _, delivery := range group {
		if validateIngestDelivery(ctx, delivery) {
			candidates = append(candidates, delivery)
		}
	}
	authorized := make([]IngestDelivery, 0, len(candidates))
	releases := make([]func(), 0, len(candidates)+1)
	if len(candidates) > 1 && candidates[0].BeginMutationGroup != nil {
		var release func()
		ctx, release = candidates[0].BeginMutationGroup(ctx)
		releases = append(releases, release)
	}
	for _, delivery := range candidates {
		release, accepted := authorizeValidatedIngestDelivery(ctx, delivery)
		if !accepted {
			continue
		}
		authorized = append(authorized, delivery)
		releases = append(releases, release)
	}

	return authorized, releases
}

func releaseIngestGroup(releases []func()) {
	for index := len(releases) - 1; index >= 0; index-- {
		releases[index]()
	}
}
