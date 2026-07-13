package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const remoteSearchCapacityFailure = "remote search capacity exhausted"

var processRemoteSearchAdmission = make(chan struct{}, interactiveSearchConcurrentWork)

type remoteSearchRetentionSearcher struct {
	inner     searchcore.Searcher
	admission chan struct{}
}

func withRemoteSearchRetention(inner searchcore.Searcher) searchcore.Searcher {
	if inner == nil {
		return nil
	}

	return remoteSearchRetentionSearcher{
		inner:     inner,
		admission: processRemoteSearchAdmission,
	}
}

func (s remoteSearchRetentionSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if cause := context.Cause(ctx); cause != nil {
		return searchcore.Response{}, fmt.Errorf("retain remote search: %w", cause)
	}
	select {
	case s.admission <- struct{}{}:
		defer func() { <-s.admission }()

		response, err := s.inner.Search(ctx, req)
		if err != nil {
			return response, fmt.Errorf("retained remote search: %w", err)
		}

		return response, nil
	default:
		return searchcore.Response{
			Request: req,
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceRemoteStage,
				Reason: remoteSearchCapacityFailure,
			}},
		}, nil
	}
}
