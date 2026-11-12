package yagonode

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

var errCrawlStateMaximum = errors.New("node crawl state maximum reached")

type crawlStateGrowthAdmission struct {
	path       string
	maximum    atomic.Value
	quotaBytes atomic.Int64
	pressure   *yagocrawlcontract.StoragePressureGate
	upstream   growthAdmission
}

func newCrawlStateGrowthAdmission(
	path string,
	maximumBytes int64,
	upstream growthAdmission,
) *crawlStateGrowthAdmission {
	admission := &crawlStateGrowthAdmission{path: path, upstream: upstream}
	admission.maximum.Store(crawlStateMaximumPolicy{MaximumBytes: maximumBytes})
	if pressure, ok := upstream.(*yagocrawlcontract.StoragePressureGate); ok {
		admission.pressure = pressure
	}

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
	admission.SetMaximumPolicy(crawlStateMaximumPolicy{
		Automatic:    config.Crawl.StateMaximumAutomatic,
		MaximumBytes: config.Crawl.StateMaximumBytes,
	})
	admission.SetStorageQuota(config.StorageQuotaByte)
	config.Crawl.GrowthAdmission = admission
	toggles.SetCrawlerNodeStateMaximumSink(admission.SetMaximumPolicy)
	toggles.SetCrawlerNodeStateQuotaSink(admission.SetStorageQuota)
}

func (admission *crawlStateGrowthAdmission) CheckGrowth() error {
	if admission.upstream != nil {
		if err := admission.upstream.CheckGrowth(); err != nil {
			return fmt.Errorf("check upstream storage growth: %w", err)
		}
	}
	maximumBytes := admission.CrawlStateMaximumBytes()
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
	admission.SetMaximumPolicy(crawlStateMaximumPolicy{MaximumBytes: maximumBytes})
}

func (admission *crawlStateGrowthAdmission) SetMaximumPolicy(policy crawlStateMaximumPolicy) {
	admission.maximum.Store(policy)
}

func (admission *crawlStateGrowthAdmission) SetStorageQuota(quotaBytes int64) {
	if quotaBytes < 0 {
		return
	}
	admission.quotaBytes.Store(quotaBytes)
}

func (admission *crawlStateGrowthAdmission) CrawlStateMaximumBytes() int64 {
	policy, _ := admission.maximum.Load().(crawlStateMaximumPolicy)
	if !policy.Automatic {
		return policy.MaximumBytes
	}

	return crawlStateAutomaticMaximumBytes(admission.quotaBytes.Load(), admission.pressure)
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
