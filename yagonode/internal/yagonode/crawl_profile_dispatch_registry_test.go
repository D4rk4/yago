package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlformats"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type crawlProfileWriterProbe struct {
	profiles []yagocrawlcontract.CrawlProfile
	failures int
}

func (p *crawlProfileWriterProbe) RecordProfile(
	_ context.Context,
	profile yagocrawlcontract.CrawlProfile,
) error {
	p.profiles = append(p.profiles, profile)
	if p.failures > 0 {
		p.failures--

		return errors.New("profile write failed")
	}

	return nil
}

type crawlProfileQueueProbe struct {
	orders []yagocrawlcontract.CrawlOrder
	err    error
}

func (p *crawlProfileQueueProbe) PublishOnce(
	_ context.Context,
	_ string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	p.orders = append(p.orders, order)

	return false, p.err
}

func TestCrawlProfileDispatchRegistryCachesOnlySuccessfulProfiles(t *testing.T) {
	writer := &crawlProfileWriterProbe{failures: 1}
	inner := &crawlProfileQueueProbe{}
	queue := crawlProfileRegisteringQueue{
		inner:    inner,
		registry: newCrawlProfileDispatchRegistry(writer),
	}
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:           "cached profile",
		RecrawlIfOlder: time.Hour,
	})
	order := yagocrawlcontract.CrawlOrder{Profile: profile}
	for range 3 {
		if _, err := queue.PublishOnce(t.Context(), "key", order); err != nil {
			t.Fatalf("publish profile: %v", err)
		}
	}
	profile.RecrawlIfOlder = 2 * time.Hour
	order.Profile = profile
	if _, err := queue.PublishOnce(t.Context(), "key", order); err != nil {
		t.Fatalf("publish changed profile: %v", err)
	}
	if len(writer.profiles) != 3 {
		t.Fatalf("profile writes = %d, want failed, retry, and change", len(writer.profiles))
	}
	if len(inner.orders) != 4 {
		t.Fatalf("published orders = %d, want 4", len(inner.orders))
	}
}

func TestCrawlProfileRegistryReceivesStampedProfile(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	formats, err := crawlformats.Open(storage)
	if err != nil {
		t.Fatalf("open crawl formats: %v", err)
	}
	toggles := yagocrawlcontract.DefaultFormatToggles()
	toggles.Archives = true
	if err := formats.Set(t.Context(), toggles); err != nil {
		t.Fatalf("set crawl formats: %v", err)
	}
	writer := &crawlProfileWriterProbe{}
	inner := &crawlProfileQueueProbe{}
	queue := formatStampingQueue{
		inner: crawlProfileRegisteringQueue{
			inner:    inner,
			registry: newCrawlProfileDispatchRegistry(writer),
		},
		formats: formats,
	}
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(
			yagocrawlcontract.CrawlProfile{Name: "formatted profile"},
		),
	}
	if _, err := queue.PublishOnce(t.Context(), "key", order); err != nil {
		t.Fatalf("publish formatted profile: %v", err)
	}
	if len(writer.profiles) != 1 || writer.profiles[0].Formats != toggles {
		t.Fatalf("registered formats = %+v, want %+v", writer.profiles, toggles)
	}
	if len(inner.orders) != 1 || inner.orders[0].Profile.Formats != toggles {
		t.Fatalf("published formats = %+v, want %+v", inner.orders, toggles)
	}
}

func TestCrawlProfileRegisteringQueueWrapsPublishFailure(t *testing.T) {
	writer := &crawlProfileWriterProbe{}
	inner := &crawlProfileQueueProbe{err: errors.New("queue failed")}
	queue := crawlProfileRegisteringQueue{
		inner:    inner,
		registry: newCrawlProfileDispatchRegistry(writer),
	}
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(
			yagocrawlcontract.CrawlProfile{Name: "failed publish"},
		),
	}
	if _, err := queue.PublishOnce(t.Context(), "key", order); err == nil {
		t.Fatal("inner publish failure was hidden")
	}
}

func TestCrawlProfileDispatchRegistryEvictsOldestHandle(t *testing.T) {
	writer := &crawlProfileWriterProbe{}
	registry := newCrawlProfileDispatchRegistry(writer)
	for index := range crawlProfileDispatchRegistryCapacity + 1 {
		registry.record(t.Context(), yagocrawlcontract.CrawlProfile{
			Handle: fmt.Sprintf("profile-%03d", index),
		})
	}
	if len(registry.recorded) != crawlProfileDispatchRegistryCapacity {
		t.Fatalf(
			"cached profiles = %d, want %d",
			len(registry.recorded),
			crawlProfileDispatchRegistryCapacity,
		)
	}
	if _, found := registry.recorded["profile-000"]; found {
		t.Fatal("oldest profile remained cached")
	}
	registry.record(t.Context(), yagocrawlcontract.CrawlProfile{Handle: "profile-000"})
	if len(writer.profiles) != crawlProfileDispatchRegistryCapacity+2 {
		t.Fatalf("profile writes after eviction = %d", len(writer.profiles))
	}
	if _, found := registry.recorded["profile-000"]; !found {
		t.Fatal("evicted profile was not cached after re-registration")
	}
	if _, found := registry.recorded["profile-001"]; found {
		t.Fatal("second-oldest profile was not evicted")
	}
}
