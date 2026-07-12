package yacysearch

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func responseHTMLWithImpression(
	r *http.Request,
	response searchcore.Response,
	recorder ImpressionRecorder,
) htmlSearchPage {
	page := responseHTML(r, response)
	if recorder == nil || len(response.Results) == 0 {
		return page
	}
	candidates := impressionCandidates(response)
	prepared, err := recorder.PrepareImpression(
		r.Context(),
		response.Request.SubmittedText(),
		candidates,
	)
	if err != nil || prepared.Token == "" ||
		!validImpressionOrder(prepared.Order, len(candidates)) {
		return page
	}
	reordered := response
	reordered.Results = make([]searchcore.Result, len(response.Results))
	for position, original := range prepared.Order {
		reordered.Results[position] = response.Results[original]
	}
	page = responseHTML(r, reordered)
	page.ClickCapture = true
	page.ImpressionToken = prepared.Token

	return page
}

func impressionCandidates(response searchcore.Response) []ImpressionCandidate {
	candidates := make([]ImpressionCandidate, len(response.Results))
	lexicalPositions := searchcore.LexicalPositions(response.Results, response.Request.Offset)
	for index, result := range response.Results {
		clusterIdentity := result.ClusterID
		if clusterIdentity == "" {
			clusterIdentity = result.URLHash
		}
		if clusterIdentity == "" {
			clusterIdentity = result.URL
		}
		candidates[index] = ImpressionCandidate{
			URLIdentity:     result.URL,
			ClusterIdentity: clusterIdentity,
			Position:        response.Request.Offset + index + 1,
			LexicalPosition: lexicalPositions[index],
		}
	}

	return candidates
}

func validImpressionOrder(order []int, length int) bool {
	if len(order) != length {
		return false
	}
	seen := make([]bool, length)
	for _, index := range order {
		if index < 0 || index >= length || seen[index] {
			return false
		}
		seen[index] = true
	}

	return true
}
