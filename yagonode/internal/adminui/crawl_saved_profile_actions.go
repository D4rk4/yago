package adminui

import (
	"fmt"
	"net/http"
	"strings"
)

func (c *Console) selectedSavedCrawlForm(r *http.Request) (crawlForm, error) {
	form := defaultCrawlFormFor(c.crawl)
	identity := strings.TrimSpace(r.URL.Query().Get("profile"))
	if identity == "" {
		return form, nil
	}
	if c.savedCrawlProfiles == nil {
		return form, fmt.Errorf("saved crawl profiles are unavailable")
	}
	profile, err := c.savedCrawlProfiles.Profile(r.Context(), identity)
	if err != nil {
		return form, fmt.Errorf("load saved crawl profile: %w", err)
	}
	form = crawlFormFromSaved(profile)
	form.Mode = "url"

	return form, nil
}

func (c *Console) handleSavedCrawlProfile(w http.ResponseWriter, r *http.Request) {
	if c.savedCrawlProfiles == nil {
		http.NotFound(w, r)

		return
	}
	action := r.PostFormValue("action")
	identity := strings.TrimSpace(r.PostFormValue("profileId"))
	form := parseCrawlForm(r)
	var saved SavedCrawlProfileView
	var err error
	switch action {
	case "create":
		saved, err = c.savedCrawlProfiles.CreateProfile(r.Context(), crawlStartFromForm(form))
	case "update":
		saved, err = c.savedCrawlProfiles.UpdateProfile(
			r.Context(),
			identity,
			crawlStartFromForm(form),
		)
	case "delete":
		err = c.savedCrawlProfiles.DeleteProfile(r.Context(), identity)
	default:
		http.Error(w, "unknown saved crawl profile action", http.StatusBadRequest)

		return
	}
	if err != nil {
		if action == "delete" {
			form = defaultCrawlFormFor(c.crawl)
		}
		data := c.crawlPage(r, form)
		data.ProfileError = err.Error()
		data.SelectedProfileID = identity
		c.render(r.Context(), w, c.tpl.crawl, "layout", data)

		return
	}
	redirectIdentity := ""
	if action != "delete" {
		redirectIdentity = saved.ID
	}
	redirectToSavedCrawlProfile(w, redirectIdentity)
}
