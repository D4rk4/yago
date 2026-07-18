package adminauth

import (
	"fmt"
	"net/url"
	"strconv"
)

func parseAPIKeyPageRequest(values url.Values) (APIKeyPageRequest, error) {
	if len(values["cursor"]) > 1 || len(values["limit"]) > 1 {
		return APIKeyPageRequest{}, fmt.Errorf("cursor and limit may be supplied once")
	}
	request := APIKeyPageRequest{
		Cursor: values.Get("cursor"),
		Limit:  maximumAPIKeys,
	}
	if !ValidAPIKeyPageCursor(request.Cursor) {
		return APIKeyPageRequest{}, fmt.Errorf("cursor is not a valid API key identifier")
	}
	if values.Has("limit") {
		limit, err := strconv.Atoi(values.Get("limit"))
		if err != nil || limit < 1 || limit > maximumAPIKeys {
			return APIKeyPageRequest{}, fmt.Errorf(
				"limit must be an integer from 1 to %d",
				maximumAPIKeys,
			)
		}
		request.Limit = limit
	}

	return request, nil
}
