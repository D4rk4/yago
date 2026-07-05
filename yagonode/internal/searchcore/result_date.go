package searchcore

import "time"

// DisplayDate renders the result's compact yyyymmdd date in the canonical human
// form the search surfaces show ("Mon, 02 Jan 2006", the YaCy result style);
// unknown or malformed dates render empty so the meta line simply omits them.
func (r Result) DisplayDate() string {
	parsed, err := time.Parse("20060102", r.Date)
	if err != nil {
		return ""
	}

	return parsed.Format("Mon, 02 Jan 2006")
}
