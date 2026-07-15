package publicportal

import "net/http"

type portalSearchWindow struct {
	Query  string
	Dom    string
	Page   int
	Offset int
	Shown  int
	Total  int
}

func redirectPortalSearchWindow(
	w http.ResponseWriter,
	r *http.Request,
	window portalSearchWindow,
) bool {
	canonicalPage, redirect := canonicalPortalPage(window.Page, window.Shown, window.Total)
	if !redirect {
		return false
	}

	http.Redirect(
		w,
		r,
		portalPageURL(window.Query, window.Dom, canonicalPage),
		http.StatusSeeOther,
	)

	return true
}
