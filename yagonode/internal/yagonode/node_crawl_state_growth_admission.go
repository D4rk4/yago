package yagonode

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
)

var errCrawlStateMaximum = errors.New("node crawl state maximum reached")

type crawlStateGrowthAdmission struct {
	path         string
	maximumBytes atomic.Int64
	upstream     growthAdmission
}

func newCrawlStateGrowthAdmission(
	path string,
	maximumBytes int64,
	upstream growthAdmission,
) *crawlStateGrowthAdmission {
	admission := &crawlStateGrowthAdmission{path: path, upstream: upstream}
	admission.maximumBytes.Store(maximumBytes)

	return admission
}

func configureCrawlStateGrowthAdmission(
	config *nodeConfig,
	toggles *runtimeToggles,
	upstream growthAdmission,
) {
	admission := newCrawlStateGrowthAdmission(
		config.Crawl.StatePath,
		config.Crawl.StateMaximumBytes,
		upstream,
	)
	config.Crawl.GrowthAdmission = admission
	toggles.SetCrawlerNodeStateMaximumSink(admission.SetMaximumBytes)
}

func (admission *crawlStateGrowthAdmission) CheckGrowth() error {
	if admission.upstream != nil {
		if err := admission.upstream.CheckGrowth(); err != nil {
			return fmt.Errorf("check upstream storage growth: %w", err)
		}
	}
	maximumBytes := admission.maximumBytes.Load()
	if maximumBytes == 0 {
		return nil
	}
	info, err := os.Stat(admission.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect node crawl state: %w", err)
	}
	if info.Size() >= maximumBytes {
		return errCrawlStateMaximum
	}

	return nil
}

func (admission *crawlStateGrowthAdmission) SetMaximumBytes(maximumBytes int64) {
	admission.maximumBytes.Store(max(maximumBytes, 0))
}

func (admission *crawlStateGrowthAdmission) CrawlStateMaximumBytes() int64 {
	return admission.maximumBytes.Load()
}

func (admission *crawlStateGrowthAdmission) CrawlStateLifecycleAdmission() growthAdmission {
	return admission.upstream
}

type crawlStateAdmissionPolicy interface {
	CrawlStateMaximumBytes() int64
	CrawlStateLifecycleAdmission() growthAdmission
}

func crawlStateMaximumBytes(admission growthAdmission) int64 {
	policy, ok := admission.(crawlStateAdmissionPolicy)
	if !ok {
		return 0
	}

	return policy.CrawlStateMaximumBytes()
}

func crawlStateLifecycleAdmission(admission growthAdmission) growthAdmission {
	policy, ok := admission.(crawlStateAdmissionPolicy)
	if !ok {
		return admission
	}

	return policy.CrawlStateLifecycleAdmission()
}
