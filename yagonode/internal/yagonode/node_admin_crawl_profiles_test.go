package yagonode

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/crawlprofilelibrary"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func savedProfileSourceFixture(t *testing.T) (savedCrawlProfileSource, *events.Recorder) {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	library, err := crawlprofilelibrary.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := crawldispatch.NewDispatcher(
		yagomodel.Hash("ABCDEFGHIJKL"), nil, nil,
		crawldispatch.WithMaxPagesPerRun(func() int { return 900 }),
	)
	recorder := events.NewRecorder(8)

	return newSavedCrawlProfileSource(
		library,
		dispatcher,
		recorder,
	).(savedCrawlProfileSource), recorder
}

func savedProfileRequest(name string) adminui.CrawlStart {
	maximum := 700
	return adminui.CrawlStart{
		Name: name, Scope: "domain", MaxDepth: 4,
		URLMustMatch: ".*", IndexURLMustMatch: ".*",
		MaxPagesPerHost: 250, MaxPagesPerRun: &maximum,
		AllowQueryURLs: true, CrawlDelay: "10s",
	}
}

func TestSavedCrawlProfileSourceLifecycleUsesOperatorValidation(t *testing.T) {
	source, recorder := savedProfileSourceFixture(t)
	created, err := source.CreateProfile(t.Context(), savedProfileRequest("Docs"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Docs" || created.Profile.MaxPagesPerHost != 250 ||
		created.Profile.MaxPagesPerRun == nil || *created.Profile.MaxPagesPerRun != 700 {
		t.Fatalf("created profile = %+v", created)
	}
	loaded, err := source.Profile(t.Context(), created.ID)
	if err != nil || loaded.ID != created.ID {
		t.Fatalf("loaded profile = %+v, %v", loaded, err)
	}
	update := savedProfileRequest("Reference")
	update.MaxDepth = 5
	updated, err := source.UpdateProfile(t.Context(), created.ID, update)
	if err != nil || updated.Name != "Reference" || updated.MaxDepth != 5 {
		t.Fatalf("updated profile = %+v, %v", updated, err)
	}
	profiles, err := source.Profiles(t.Context())
	if err != nil || len(profiles) != 1 || profiles[0].ID != created.ID {
		t.Fatalf("profile list = %+v, %v", profiles, err)
	}
	if err := source.DeleteProfile(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	eventLog := recorder.Recent(3)
	if len(eventLog) != 3 || eventLog[0].Name != eventSavedCrawlProfileDeleted ||
		eventLog[1].Name != eventSavedCrawlProfileUpdated ||
		eventLog[2].Name != eventSavedCrawlProfileCreated {
		t.Fatalf("profile events = %+v", eventLog)
	}
}

func TestSavedCrawlProfileSourceRejectsInvalidDefinition(t *testing.T) {
	source, recorder := savedProfileSourceFixture(t)
	request := savedProfileRequest("Broken")
	request.URLMustMatch = "["
	if _, err := source.CreateProfile(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "validate saved crawl profile") {
		t.Fatalf("invalid definition error = %v", err)
	}
	if len(recorder.Recent(1)) != 0 {
		t.Fatal("failed profile creation emitted a success event")
	}
}

func TestSavedCrawlProfileSourceRequiresLibraryAndDispatcher(t *testing.T) {
	if newSavedCrawlProfileSource(nil, nil, nil) != nil {
		t.Fatal("saved profile source exists without dependencies")
	}
}

func TestSavedCrawlProfileSourceSurfacesLibraryAndValidationFailures(t *testing.T) {
	source, recorder := savedProfileSourceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := source.Profiles(ctx); err == nil {
		t.Fatal("canceled profile list succeeded")
	}
	if _, err := source.Profile(t.Context(), "bad"); err == nil {
		t.Fatal("invalid profile identity was loaded")
	}
	created, err := source.CreateProfile(t.Context(), savedProfileRequest("Duplicate"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreateProfile(t.Context(), savedProfileRequest(" duplicate ")); err == nil {
		t.Fatal("duplicate profile name was created")
	}
	invalid := savedProfileRequest("Invalid update")
	invalid.URLMustMatch = "["
	if _, err := source.UpdateProfile(t.Context(), created.ID, invalid); err == nil {
		t.Fatal("invalid profile update succeeded")
	}
	if _, err := source.UpdateProfile(
		t.Context(),
		strings.Repeat("0", 32),
		savedProfileRequest("Missing"),
	); err == nil {
		t.Fatal("missing profile update succeeded")
	}
	if err := source.DeleteProfile(t.Context(), strings.Repeat("0", 32)); err == nil {
		t.Fatal("missing profile delete succeeded")
	}
	if len(recorder.Recent(8)) != 1 {
		t.Fatalf("success events after failures = %+v", recorder.Recent(8))
	}
}

func TestSavedCrawlProfileViewFormatsUnlimitedScopeAndDurations(t *testing.T) {
	maximum := 0
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name: "Wide", Scope: yagocrawlcontract.ScopeWide,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		MaxPagesPerRun:  &maximum, RecrawlIfOlder: 24 * time.Hour, CrawlDelay: 2 * time.Second,
	})
	view := savedCrawlProfileView(crawlprofilelibrary.SavedProfile{
		ID: "profile", Profile: profile, UpdatedAt: time.Unix(100, 0).UTC(),
	})
	if view.Scope != "wide" || view.Profile.MaxPagesPerHost != 0 ||
		view.Profile.RecrawlIfOlder != "1d" || view.Profile.CrawlDelay != "2s" {
		t.Fatalf("wide profile view = %+v", view)
	}
	if crawlScopeName(yagocrawlcontract.ScopeSubpath) != "subpath" ||
		crawlScopeName(yagocrawlcontract.ScopeDomain) != "domain" || durationText(0) != "" {
		t.Fatal("scope or empty duration formatting changed")
	}
}

type savedProfileAdminCrawlProcess struct {
	dispatch *crawldispatch.Dispatcher
}

func (*savedProfileAdminCrawlProcess) mountDispatch(*http.ServeMux) {}

func (*savedProfileAdminCrawlProcess) Run(context.Context) {}

func (*savedProfileAdminCrawlProcess) Close() {}

func (process *savedProfileAdminCrawlProcess) dispatcher() *crawldispatch.Dispatcher {
	return process.dispatch
}

func TestSavedCrawlProfileAdminWiringReportsDuplicateRegistration(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	dispatcher := crawldispatch.NewDispatcher(
		yagomodel.Hash("ABCDEFGHIJKL"),
		nil,
		nil,
		crawldispatch.WithMaxPagesPerRun(func() int { return 900 }),
	)
	assembled := node{
		vault: storage,
		crawl: &savedProfileAdminCrawlProcess{dispatch: dispatcher},
	}
	options := &adminui.Options{}
	applySavedCrawlProfileAdminOptions(options, assembled, nil)
	if options.SavedCrawlProfiles == nil {
		t.Fatal("saved crawl profile source was not wired")
	}
	recorder := events.NewRecorder(4)
	failed := &adminui.Options{}
	applySavedCrawlProfileAdminOptions(failed, assembled, recorder)
	if failed.SavedCrawlProfiles != nil {
		t.Fatal("duplicate profile library registration was wired")
	}
	log := recorder.Recent(1)
	if len(log) != 1 || log[0].Name != "crawl.profile.unavailable" {
		t.Fatalf("profile wiring events = %+v", log)
	}
	applySavedCrawlProfileAdminOptions(&adminui.Options{}, assembled, nil)
	applySavedCrawlProfileAdminOptions(&adminui.Options{}, node{vault: storage}, recorder)
	applySavedCrawlProfileAdminOptions(
		&adminui.Options{},
		node{crawl: &savedProfileAdminCrawlProcess{dispatch: dispatcher}},
		recorder,
	)
}
