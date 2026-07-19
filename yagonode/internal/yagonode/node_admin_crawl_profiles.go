package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/crawlprofilelibrary"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

const (
	eventSavedCrawlProfileCreated = "crawl.profile.created"
	eventSavedCrawlProfileUpdated = "crawl.profile.updated"
	eventSavedCrawlProfileDeleted = "crawl.profile.deleted"
)

type savedCrawlProfileSource struct {
	library    *crawlprofilelibrary.Library
	dispatcher *crawldispatch.Dispatcher
	recorder   *events.Recorder
}

func newSavedCrawlProfileSource(
	library *crawlprofilelibrary.Library,
	dispatcher *crawldispatch.Dispatcher,
	recorder *events.Recorder,
) adminui.SavedCrawlProfileSource {
	if library == nil || dispatcher == nil {
		return nil
	}

	return savedCrawlProfileSource{library: library, dispatcher: dispatcher, recorder: recorder}
}

func (source savedCrawlProfileSource) Profiles(
	ctx context.Context,
) ([]adminui.SavedCrawlProfileView, error) {
	profiles, err := source.library.Profiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list saved crawl profiles: %w", err)
	}
	views := make([]adminui.SavedCrawlProfileView, 0, len(profiles))
	for _, profile := range profiles {
		views = append(views, savedCrawlProfileView(profile))
	}

	return views, nil
}

func (source savedCrawlProfileSource) Profile(
	ctx context.Context,
	identity string,
) (adminui.SavedCrawlProfileView, error) {
	profile, err := source.library.Profile(ctx, identity)
	if err != nil {
		return adminui.SavedCrawlProfileView{}, fmt.Errorf("read saved crawl profile: %w", err)
	}

	return savedCrawlProfileView(profile), nil
}

func (source savedCrawlProfileSource) CreateProfile(
	ctx context.Context,
	request adminui.CrawlStart,
) (adminui.SavedCrawlProfileView, error) {
	profile, err := source.crawlProfile(request)
	if err != nil {
		return adminui.SavedCrawlProfileView{}, err
	}
	saved, err := source.library.Create(ctx, profile)
	if err != nil {
		return adminui.SavedCrawlProfileView{}, fmt.Errorf("create saved crawl profile: %w", err)
	}
	source.record(eventSavedCrawlProfileCreated, "saved crawl profile created")

	return savedCrawlProfileView(saved), nil
}

func (source savedCrawlProfileSource) UpdateProfile(
	ctx context.Context,
	identity string,
	request adminui.CrawlStart,
) (adminui.SavedCrawlProfileView, error) {
	profile, err := source.crawlProfile(request)
	if err != nil {
		return adminui.SavedCrawlProfileView{}, err
	}
	saved, err := source.library.Update(ctx, identity, profile)
	if err != nil {
		return adminui.SavedCrawlProfileView{}, fmt.Errorf("update saved crawl profile: %w", err)
	}
	source.record(eventSavedCrawlProfileUpdated, "saved crawl profile updated")

	return savedCrawlProfileView(saved), nil
}

func (source savedCrawlProfileSource) DeleteProfile(
	ctx context.Context,
	identity string,
) error {
	if err := source.library.Delete(ctx, identity); err != nil {
		return fmt.Errorf("delete saved crawl profile: %w", err)
	}
	source.record(eventSavedCrawlProfileDeleted, "saved crawl profile deleted")

	return nil
}

func (source savedCrawlProfileSource) crawlProfile(
	request adminui.CrawlStart,
) (yagocrawlcontract.CrawlProfile, error) {
	profile, err := (crawldispatch.OperatorRequest{
		Name:                     request.Name,
		Scope:                    request.Scope,
		URLMustMatch:             request.URLMustMatch,
		URLMustNotMatch:          request.URLMustNotMatch,
		IndexURLMustMatch:        request.IndexURLMustMatch,
		IndexURLMustNotMatch:     request.IndexURLMustNotMatch,
		MaxDepth:                 request.MaxDepth,
		AllowQueryURLs:           request.AllowQueryURLs,
		IgnoreTLSAuthority:       request.IgnoreTLSAuthority,
		IgnoreRobots:             request.IgnoreRobots,
		DisableBrowser:           request.DisableBrowser,
		FollowNoFollowLinks:      request.FollowNoFollowLinks,
		NoindexCanonicalMismatch: request.NoindexCanonicalMismatch,
		MaxPagesPerHost:          pagesPerHostOrUnlimited(request.MaxPagesPerHost),
		MaxPagesPerRun:           request.MaxPagesPerRun,
		RecrawlIfOlder:           request.RecrawlIfOlder,
		CrawlDelay:               request.CrawlDelay,
	}).Profile(source.dispatcher.MaxPagesPerRun())
	if err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf("validate saved crawl profile: %w", err)
	}

	return profile, nil
}

func (source savedCrawlProfileSource) record(name, message string) {
	if source.recorder != nil {
		source.recorder.Record(events.SeverityInfo, events.CategoryCrawl, name, message)
	}
}

func savedCrawlProfileView(
	saved crawlprofilelibrary.SavedProfile,
) adminui.SavedCrawlProfileView {
	profile := saved.Profile
	maximum := profile.EffectiveMaxPagesPerRun(0)
	maximumPerHost := profile.MaxPagesPerHost
	if maximumPerHost == yagocrawlcontract.UnlimitedPagesPerHost {
		maximumPerHost = 0
	}
	return adminui.SavedCrawlProfileView{
		ID: saved.ID, Name: profile.Name, Scope: crawlScopeName(profile.Scope),
		MaxDepth: profile.MaxDepth, UpdatedAt: saved.UpdatedAt.UTC().Format(time.RFC3339),
		Profile: adminui.CrawlStart{
			Name:                     profile.Name,
			Scope:                    crawlScopeName(profile.Scope),
			MaxDepth:                 profile.MaxDepth,
			URLMustMatch:             profile.URLMustMatch,
			URLMustNotMatch:          profile.URLMustNotMatch,
			IndexURLMustMatch:        profile.IndexURLMustMatch,
			IndexURLMustNotMatch:     profile.IndexURLMustNotMatch,
			MaxPagesPerHost:          maximumPerHost,
			MaxPagesPerRun:           &maximum,
			AllowQueryURLs:           profile.AllowQueryURLs,
			FollowNoFollowLinks:      profile.FollowNoFollowLinks,
			NoindexCanonicalMismatch: profile.NoindexCanonicalMismatch,
			IgnoreTLSAuthority:       profile.IgnoreTLSAuthority,
			IgnoreRobots:             profile.IgnoreRobots,
			DisableBrowser:           profile.DisableBrowser,
			RecrawlIfOlder: yagocrawlcontract.FormatRecrawlInterval(
				profile.RecrawlIfOlder,
			),
			CrawlDelay: durationText(profile.CrawlDelay),
		},
	}
}

func crawlScopeName(scope yagocrawlcontract.CrawlScope) string {
	switch scope {
	case yagocrawlcontract.ScopeWide:
		return "wide"
	case yagocrawlcontract.ScopeSubpath:
		return "subpath"
	default:
		return "domain"
	}
}

func durationText(duration time.Duration) string {
	if duration == 0 {
		return ""
	}

	return duration.String()
}
