package yacyproto

type SearchAbstracts string

const SearchAbstractsAuto SearchAbstracts = "auto"

func parseSearchAbstracts(raw string) (SearchAbstracts, error) {
	if raw == "" || raw == string(SearchAbstractsAuto) {
		return SearchAbstracts(raw), nil
	}

	if _, err := splitSearchHashes(FieldAbstracts, raw); err != nil {
		return "", err
	}

	return SearchAbstracts(raw), nil
}
