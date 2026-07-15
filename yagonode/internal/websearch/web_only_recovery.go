package websearch

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func clearPrimaryMissRecoveryForWebAnswer(
	response *searchcore.Response,
	webResults []searchcore.Result,
) {
	if len(response.Results) > 0 || len(webResults) == 0 {
		return
	}
	response.Recovered = ""
	response.DidYouMean = ""
}
