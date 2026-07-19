package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeSavedCrawlProfiles struct {
	profiles    []SavedCrawlProfileView
	selected    SavedCrawlProfileView
	created     CrawlStart
	updated     CrawlStart
	updatedID   string
	deletedID   string
	createErr   error
	profilesErr error
	profileErr  error
	updateErr   error
	deleteErr   error
}

func (source *fakeSavedCrawlProfiles) Profiles(context.Context) ([]SavedCrawlProfileView, error) {
	if source.profilesErr != nil {
		return nil, source.profilesErr
	}

	return append([]SavedCrawlProfileView(nil), source.profiles...), nil
}

func (source *fakeSavedCrawlProfiles) Profile(
	_ context.Context,
	identity string,
) (SavedCrawlProfileView, error) {
	if source.profileErr != nil {
		return SavedCrawlProfileView{}, source.profileErr
	}
	if identity != source.selected.ID {
		return SavedCrawlProfileView{}, errors.New("profile not found")
	}

	return source.selected, nil
}

func (source *fakeSavedCrawlProfiles) CreateProfile(
	_ context.Context,
	request CrawlStart,
) (SavedCrawlProfileView, error) {
	source.created = request
	if source.createErr != nil {
		return SavedCrawlProfileView{}, source.createErr
	}

	return SavedCrawlProfileView{ID: "created"}, nil
}

func (source *fakeSavedCrawlProfiles) UpdateProfile(
	_ context.Context,
	identity string,
	request CrawlStart,
) (SavedCrawlProfileView, error) {
	source.updatedID = identity
	source.updated = request
	if source.updateErr != nil {
		return SavedCrawlProfileView{}, source.updateErr
	}

	return SavedCrawlProfileView{ID: identity}, nil
}

func (source *fakeSavedCrawlProfiles) DeleteProfile(
	_ context.Context,
	identity string,
) error {
	source.deletedID = identity
	if source.deleteErr != nil {
		return source.deleteErr
	}

	return nil
}

func savedCrawlProfileFixture() SavedCrawlProfileView {
	maximum := 900
	return SavedCrawlProfileView{
		ID: "profile-1", Name: "Reference crawl", Scope: "domain", MaxDepth: 5,
		UpdatedAt: "2026-07-18T12:00:00Z",
		Profile: CrawlStart{
			Name: "Reference crawl", Scope: "domain", MaxDepth: 5,
			URLMustMatch: ".*", IndexURLMustMatch: ".*",
			MaxPagesPerHost: 250, MaxPagesPerRun: &maximum,
			AllowQueryURLs: true, CrawlDelay: "10s",
		},
	}
}

func TestConsoleAppliesSavedCrawlProfileWithoutPersistedSeeds(t *testing.T) {
	profile := savedCrawlProfileFixture()
	source := &fakeSavedCrawlProfiles{profiles: []SavedCrawlProfileView{profile}, selected: profile}
	console := New(Options{Crawl: &fakeCrawl{}, SavedCrawlProfiles: source})
	got := do(t, console, "/admin/crawl?profile=profile-1")
	for _, want := range []string{
		"Saved crawl profiles", "future dispatches only", "Reference crawl",
		`name="profileId" value="profile-1"`, `name="maxDepth" min="0" max="64" value="5"`,
		`name="maxPagesPerHost" min="0" value="250"`,
		`name="maxPagesPerRun" min="0" value="900"`, "Update saved profile",
		`id="seeds" name="seeds" rows="4" autocomplete="off" required></textarea>`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("saved profile page missing %q", want)
		}
	}
}

func TestConsoleCreatesUpdatesAndDeletesSavedCrawlProfiles(t *testing.T) {
	source := &fakeSavedCrawlProfiles{}
	console := New(Options{Crawl: &fakeCrawl{}, SavedCrawlProfiles: source})
	definition := url.Values{
		"action": {"create"}, "name": {"Docs"}, "scope": {"domain"},
		"maxDepth": {"4"}, "maxPagesPerHost": {"250"}, "maxPagesPerRun": {"900"},
		"seeds": {"https://must-not-persist.example/"},
	}
	created := doPost(t, console, "/admin/crawl/profile", definition)
	if created.status != http.StatusSeeOther ||
		created.header.Get("Location") != "/admin/crawl?profile=created" {
		t.Fatalf("create response = %d %q", created.status, created.header.Get("Location"))
	}
	if source.created.Name != "Docs" || source.created.MaxPagesPerHost != 250 ||
		source.created.MaxPagesPerRun == nil || *source.created.MaxPagesPerRun != 900 ||
		len(source.created.Seeds) != 0 {
		t.Fatalf("created profile = %+v", source.created)
	}
	definition.Set("action", "update")
	definition.Set("profileId", "profile-1")
	updated := doPost(t, console, "/admin/crawl/profile", definition)
	if updated.status != http.StatusSeeOther || source.updatedID != "profile-1" ||
		source.updated.Name != "Docs" {
		t.Fatalf("updated profile = %d %q %+v", updated.status, source.updatedID, source.updated)
	}
	deleted := doPost(t, console, "/admin/crawl/profile", url.Values{
		"action": {"delete"}, "profileId": {"profile-1"},
	})
	if deleted.status != http.StatusSeeOther || source.deletedID != "profile-1" {
		t.Fatalf("deleted profile = %d %q", deleted.status, source.deletedID)
	}
}

func TestConsoleShowsSavedCrawlProfileValidationFailure(t *testing.T) {
	source := &fakeSavedCrawlProfiles{
		createErr: errors.New("saved crawl profile name already exists"),
	}
	console := New(Options{Crawl: &fakeCrawl{}, SavedCrawlProfiles: source})
	got := doPost(t, console, "/admin/crawl/profile", url.Values{
		"action": {"create"}, "name": {"Docs"}, "scope": {"domain"},
		"maxDepth": {"3"}, "maxPagesPerRun": {"900"},
	})
	if got.status != http.StatusOK || !strings.Contains(got.body, "name already exists") {
		t.Fatalf("validation response = %d %q", got.status, got.body)
	}
}

func TestConsoleSavedCrawlProfileActionsReportUnavailableAndInvalidRequests(t *testing.T) {
	withoutProfiles := New(Options{Crawl: &fakeCrawl{}})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/admin/crawl?profile=missing",
		nil,
	)
	if _, err := withoutProfiles.selectedSavedCrawlForm(request); err == nil ||
		!strings.Contains(err.Error(), "saved crawl profiles are unavailable") {
		t.Fatalf("unavailable selection error = %v", err)
	}
	post := doPost(t, withoutProfiles, "/admin/crawl/profile", url.Values{
		"action": {"create"},
	})
	if post.status != http.StatusNotFound {
		t.Fatalf("unavailable action status = %d", post.status)
	}

	profileFailure := &fakeSavedCrawlProfiles{profileErr: errors.New("profile read failed")}
	console := New(Options{Crawl: &fakeCrawl{}, SavedCrawlProfiles: profileFailure})
	loaded := do(t, console, "/admin/crawl?profile=missing")
	if loaded.status != http.StatusOK || !strings.Contains(loaded.body, "profile read failed") {
		t.Fatalf("failed selection = %d %q", loaded.status, loaded.body)
	}
	listingFailure := &fakeSavedCrawlProfiles{profilesErr: errors.New("profile list failed")}
	listed := do(
		t,
		New(Options{Crawl: &fakeCrawl{}, SavedCrawlProfiles: listingFailure}),
		"/admin/crawl",
	)
	if listed.status != http.StatusOK ||
		!strings.Contains(listed.body, "Saved crawl profiles are unavailable.") {
		t.Fatalf("failed profile listing = %d %q", listed.status, listed.body)
	}
	unknown := doPost(t, console, "/admin/crawl/profile", url.Values{
		"action": {"unknown"},
	})
	if unknown.status != http.StatusBadRequest ||
		!strings.Contains(unknown.body, "unknown saved crawl profile action") {
		t.Fatalf("unknown action = %d %q", unknown.status, unknown.body)
	}
}

func TestConsoleSavedCrawlProfileDeleteFailureReturnsCleanForm(t *testing.T) {
	source := &fakeSavedCrawlProfiles{deleteErr: errors.New("delete failed")}
	console := New(Options{Crawl: &fakeCrawl{}, SavedCrawlProfiles: source})
	got := doPost(t, console, "/admin/crawl/profile", url.Values{
		"action": {"delete"}, "profileId": {"profile-1"},
	})
	if got.status != http.StatusOK || !strings.Contains(got.body, "delete failed") ||
		!strings.Contains(got.body, `name="profileId" value="profile-1"`) {
		t.Fatalf("delete failure = %d %q", got.status, got.body)
	}
}
