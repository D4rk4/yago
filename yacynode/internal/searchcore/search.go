package searchcore

import "context"

type Source string

const (
	SourceLocal  Source = "local"
	SourceGlobal Source = "global"
	SourceRemote Source = "remote"
)

type ContentDomain string

const (
	ContentDomainAll   ContentDomain = "all"
	ContentDomainText  ContentDomain = "text"
	ContentDomainImage ContentDomain = "image"
	ContentDomainAudio ContentDomain = "audio"
	ContentDomainVideo ContentDomain = "video"
	ContentDomainApp   ContentDomain = "app"
)

type VerifyMode string

const (
	VerifyFalse     VerifyMode = "false"
	VerifyTrue      VerifyMode = "true"
	VerifyCacheOnly VerifyMode = "cacheonly"
	VerifyIfFresh   VerifyMode = "iffresh"
	VerifyIfExist   VerifyMode = "ifexist"
)

type Request struct {
	Query            string
	Terms            []string
	ExcludedTerms    []string
	Source           Source
	Limit            int
	Offset           int
	ContentDomain    ContentDomain
	Language         string
	SiteHost         string
	InURL            string
	TLD              string
	FileType         string
	URLMaskFilter    string
	PreferMaskFilter string
	Verify           VerifyMode
	Navigation       string
	SortByDate       bool
	Near             bool
}

type Result struct {
	Title         string
	URL           string
	DisplayURL    string
	Snippet       string
	Score         float64
	Source        Source
	Host          string
	Path          string
	File          string
	URLHash       string
	Size          int
	Date          string
	ContentDomain ContentDomain
	Language      string
}

type PartialFailure struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type Response struct {
	Request         Request
	TotalResults    int
	Results         []Result
	PartialFailures []PartialFailure
}

type Searcher interface {
	Search(context.Context, Request) (Response, error)
}
