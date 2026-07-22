package yagoproto

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

func parseQueryObject(raw string) QueryObject {
	return QueryObject(raw)
}
