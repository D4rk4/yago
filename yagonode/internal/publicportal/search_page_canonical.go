package publicportal

func canonicalPortalPage(requestedPage, shown, total int) (int, bool) {
	if requestedPage <= 1 {
		return 1, false
	}

	lastPage := 0
	if total > 0 {
		lastPage = total / portalPageSize
		if total%portalPageSize != 0 {
			lastPage++
		}
	}
	if lastPage == 0 {
		return 1, true
	}
	if requestedPage > lastPage {
		return lastPage, true
	}
	if shown == 0 {
		return 1, true
	}

	return requestedPage, false
}
