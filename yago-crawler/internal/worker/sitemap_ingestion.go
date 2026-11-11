package worker

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func (worker *Worker) enqueueSitemap(
	ctx context.Context,
	body []byte,
	rules runRules,
	runID string,
	depth int,
) error {
	if depth > rules.maximumDepth {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	locations := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return nil
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "loc") {
			continue
		}
		var location string
		if err := decoder.DecodeElement(&location, &start); err != nil {
			continue
		}
		if !rules.allowsCrawlURL(location) || worker.urlDenylist.Blocks(location) {
			continue
		}
		if _, _, err := worker.frontier.EnqueueRunLimited(
			ctx,
			runID,
			location,
			"",
			depth,
			time.Now(),
			rules.maximumHostPages,
			rules.maximumPages,
		); err != nil {
			if errors.Is(err, frontier.ErrRunPageLimit) {
				return nil
			}
			if !errors.Is(err, frontier.ErrHostPageLimit) &&
				!errors.Is(err, frontier.ErrPendingLimit) && !errors.Is(err, frontier.ErrStateLimit) {
				return err
			}
		}
		locations++
		if locations >= worker.policy.SitemapURLLimit {
			return nil
		}
	}
}
