package adminui

import "context"

type SavedCrawlProfileView struct {
	ID        string
	Name      string
	Scope     string
	MaxDepth  int
	UpdatedAt string
	Profile   CrawlStart
}

type SavedCrawlProfileSource interface {
	Profiles(context.Context) ([]SavedCrawlProfileView, error)
	Profile(context.Context, string) (SavedCrawlProfileView, error)
	CreateProfile(context.Context, CrawlStart) (SavedCrawlProfileView, error)
	UpdateProfile(context.Context, string, CrawlStart) (SavedCrawlProfileView, error)
	DeleteProfile(context.Context, string) error
}

func crawlStartFromForm(form crawlForm) CrawlStart {
	return CrawlStart{
		Name:                     form.Name,
		Scope:                    form.Scope,
		MaxDepth:                 form.MaxDepth,
		URLMustMatch:             form.URLMustMatch,
		URLMustNotMatch:          form.URLMustNotMatch,
		IndexURLMustMatch:        form.IndexURLMustMatch,
		IndexURLMustNotMatch:     form.IndexURLMustNotMatch,
		MaxPagesPerHost:          form.MaxPagesPerHost,
		MaxPagesPerRun:           &form.MaxPagesPerRun,
		AllowQueryURLs:           form.AllowQueryURLs,
		FollowNoFollowLinks:      form.FollowNoFollowLinks,
		NoindexCanonicalMismatch: form.NoindexCanonicalMismatch,
		IgnoreTLSAuthority:       form.IgnoreTLSAuthority,
		IgnoreRobots:             form.IgnoreRobots,
		DisableBrowser:           form.DisableBrowser,
		RecrawlIfOlder:           form.RecrawlIfOlder,
		CrawlDelay:               form.CrawlDelay,
	}
}

func crawlFormFromSaved(profile SavedCrawlProfileView) crawlForm {
	form := defaultCrawlForm()
	form.Name = profile.Profile.Name
	form.Scope = profile.Profile.Scope
	form.MaxDepth = profile.Profile.MaxDepth
	form.URLMustMatch = profile.Profile.URLMustMatch
	form.URLMustNotMatch = profile.Profile.URLMustNotMatch
	form.IndexURLMustMatch = profile.Profile.IndexURLMustMatch
	form.IndexURLMustNotMatch = profile.Profile.IndexURLMustNotMatch
	form.MaxPagesPerHost = profile.Profile.MaxPagesPerHost
	if profile.Profile.MaxPagesPerRun != nil {
		form.MaxPagesPerRun = *profile.Profile.MaxPagesPerRun
	}
	form.AllowQueryURLs = profile.Profile.AllowQueryURLs
	form.FollowNoFollowLinks = profile.Profile.FollowNoFollowLinks
	form.NoindexCanonicalMismatch = profile.Profile.NoindexCanonicalMismatch
	form.IgnoreTLSAuthority = profile.Profile.IgnoreTLSAuthority
	form.IgnoreRobots = profile.Profile.IgnoreRobots
	form.DisableBrowser = profile.Profile.DisableBrowser
	form.RecrawlIfOlder = profile.Profile.RecrawlIfOlder
	form.CrawlDelay = profile.Profile.CrawlDelay
	form.ShowExpert = true

	return form
}
