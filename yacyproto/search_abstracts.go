package yacyproto

import "github.com/D4rk4/yago/yacymodel"

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

func (a SearchAbstracts) Hashes() []yacymodel.Hash {
	hashes, err := splitSearchHashes(FieldAbstracts, string(a))
	if err != nil {
		return nil
	}

	return hashes
}
