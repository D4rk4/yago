package publicportal

import "net/http"

type portalSearchWindow struct {
	Query            string
	Dom              string
	Page             int
	Offset           int
	Shown            int
	Available        int
	Exhausted        bool
	WebSearchConsent bool
}

func redirectPortalSearchWindow(
	w http.ResponseWriter,
	r *http.Request,
	window portalSearchWindow,
) bool {
	canonicalPage, redirect := canonicalPortalPage(
		window.Page,
		window.Shown,
		window.Available,
		window.Exhausted,
	)
	if !redirect {
		return false
	}

	http.Redirect(
		w,
		r,
		portalPageURL(window.Query, window.Dom, canonicalPage, window.WebSearchConsent),
		http.StatusSeeOther,
	)

	return true
}
