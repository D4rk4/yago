package yagonode

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func (s *termSource) termSample(
	ctx context.Context,
	hash yagomodel.Hash,
) ([]adminui.TermPosting, error) {
	hashes := make([]yagomodel.Hash, 0, termSampleLimit)
	scan := func(posting yagomodel.RWIPosting) (bool, error) {
		location, err := posting.URLHash()
		if err == nil {
			hashes = append(hashes, location.Hash())
		}

		return len(hashes) < termSampleLimit, nil
	}
	scanError := s.postings.ScanWord(ctx, hash, scan)
	if len(hashes) == 0 {
		if scanError != nil {
			return nil, fmt.Errorf("scan term sample: %w", scanError)
		}

		return nil, nil
	}

	rows, rowsError := s.urls.RowsByHash(ctx, hashes)
	sample := make([]adminui.TermPosting, 0, len(rows))
	for _, row := range rows {
		rawURL, err := yagomodel.DecodeWireForm(ctx, row.Properties[yagomodel.URLMetaURL])
		if err != nil || rawURL == "" {
			continue
		}
		title, _ := row.Title(ctx)
		sample = append(sample, adminui.TermPosting{URL: rawURL, Title: title})
	}

	var sampleErrors []error
	if scanError != nil {
		sampleErrors = append(sampleErrors, fmt.Errorf("scan term sample: %w", scanError))
	}
	if rowsError != nil {
		sampleErrors = append(sampleErrors, fmt.Errorf("resolve term sample: %w", rowsError))
	}

	return sample, errors.Join(sampleErrors...)
}
