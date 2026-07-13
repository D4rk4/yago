package searchcore

func localFederatedResponse(req Request, local Response, remoteError error) Response {
	failures := append([]PartialFailure(nil), local.PartialFailures...)
	failures = append(failures, PartialFailure{
		Source: PartialFailureSourceRemoteYaCy,
		Reason: remoteError.Error(),
	})

	return Response{
		Request:         req,
		TotalResults:    local.TotalResults,
		Results:         offsetResults(local.Results, req.Offset, req.Limit),
		PartialFailures: failures,
		Facets:          local.Facets,
	}
}
