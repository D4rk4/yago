package searchsession

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func recentCoverage(
	recent RecentWindow,
	request searchcore.Request,
) (searchcore.Response, bool) {
	response, found := recent.Recent(request)
	if found || request.Source != searchcore.SourceGlobal {
		return response, found
	}
	localRequest := request
	localRequest.Source = searchcore.SourceLocal
	response, found = recent.Recent(localRequest)
	if found {
		response.Request = request
	}

	return response, found
}
