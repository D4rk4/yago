package yagoproto

import "fmt"

type QueryObject string

const (
	ObjectRWICount    QueryObject = "rwicount"
	ObjectRWIURLCount QueryObject = "rwiurlcount"
	ObjectLURLCount   QueryObject = "lurlcount"
	ObjectWantedLURLs QueryObject = "wantedlurls"
	ObjectWantedPURLs QueryObject = "wantedpurls"
	ObjectWantedWord  QueryObject = "wantedword"
	ObjectWantedRWI   QueryObject = "wantedrwi"
	ObjectWantedSeeds QueryObject = "wantedseeds"
)

func parseQueryObject(raw string) (QueryObject, error) {
	obj := QueryObject(raw)
	switch obj {
	case ObjectRWICount,
		ObjectRWIURLCount,
		ObjectLURLCount,
		ObjectWantedLURLs,
		ObjectWantedPURLs,
		ObjectWantedWord,
		ObjectWantedRWI,
		ObjectWantedSeeds:
		return obj, nil
	default:
		return "", fmt.Errorf("%w: %s=%q", ErrBadField, FieldObject, raw)
	}
}
