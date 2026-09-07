package searchsession

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func uncachedSessionPage(
	response searchcore.Response,
	request searchcore.Request,
) searchcore.Response {
	window := sessionWindow{
		results:    response.Results,
		failures:   response.PartialFailures,
		total:      advertisedTotal(response),
		recovered:  response.Recovered,
		didYouMean: response.DidYouMean,
		facets:     response.Facets,
	}

	return window.respond(request)
}
