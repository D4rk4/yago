package searchcore

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	maximumPublicQueryRunes = 512
	maximumPublicQueryTerms = 32
)

var (
	errPublicQueryTooLong         = errors.New("public query is too long")
	errPublicQueryHasTooManyTerms = errors.New("public query has too many terms")
)

func ParsePublicTextQuery(raw string) (ParsedQuery, error) {
	if err := validatePublicQueryRunes(raw); err != nil {
		return ParsedQuery{}, err
	}
	parsed := ParseTextQuery(raw)
	if err := validatePublicQueryTerms(parsed.Terms, parsed.ExcludedTerms); err != nil {
		return ParsedQuery{}, err
	}

	return parsed, nil
}

func ParsePublicRequest(req Request) (Request, error) {
	if err := validatePublicQueryRunes(req.Query); err != nil {
		return Request{}, err
	}
	if err := validatePublicQueryRunes(req.SubmittedQuery); err != nil {
		return Request{}, err
	}
	req = RequestWithParsedQuery(req)
	if err := validatePublicQueryTerms(req.Terms, req.ExcludedTerms); err != nil {
		return Request{}, err
	}

	return req, nil
}

func validatePublicQueryRunes(query string) error {
	if utf8.RuneCountInString(query) > maximumPublicQueryRunes {
		return fmt.Errorf("%w: maximum is %d runes", errPublicQueryTooLong, maximumPublicQueryRunes)
	}

	return nil
}

func validatePublicQueryTerms(terms ...[]string) error {
	total := 0
	for _, group := range terms {
		total += len(group)
	}
	if total > maximumPublicQueryTerms {
		return fmt.Errorf(
			"%w: maximum is %d terms",
			errPublicQueryHasTooManyTerms,
			maximumPublicQueryTerms,
		)
	}

	return nil
}
