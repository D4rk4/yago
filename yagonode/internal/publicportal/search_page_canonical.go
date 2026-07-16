package publicportal

func canonicalPortalPage(requestedPage, shown, available int, exhausted bool) (int, bool) {
	if requestedPage <= 1 {
		return 1, false
	}
	if shown > 0 || !exhausted {
		return requestedPage, false
	}

	lastPage := 0
	if available > 0 {
		lastPage = available / portalPageSize
		if available%portalPageSize != 0 {
			lastPage++
		}
	}
	if lastPage == 0 {
		return 1, true
	}
	if requestedPage > lastPage {
		return lastPage, true
	}
	return requestedPage, false
}
