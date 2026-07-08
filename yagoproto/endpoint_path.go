package yagoproto

type EndpointMethodSet uint8

const (
	EndpointMethodGet EndpointMethodSet = 1 << iota
	EndpointMethodPost
)

const (
	PathHello        = "/yacy/hello.html"
	PathTransferRWI  = "/yacy/transferRWI.html"
	PathTransferURL  = "/yacy/transferURL.html"
	PathSearch       = "/yacy/search.html"
	PathQuery        = "/yacy/query.html"
	PathIndex        = "/yacy/idx.json"
	PathList         = "/yacy/list.html"
	PathMessage      = "/yacy/message.html"
	PathProfile      = "/yacy/profile.html"
	PathCrawlURLs    = "/yacy/urls.xml"
	PathSeedlist     = "/yacy/seedlist.html"
	PathSeedlistJSON = "/yacy/seedlist.json"
	PathSeedlistXML  = "/yacy/seedlist.xml"
	// PathP2PSeeds and PathP2PSeedsJSON mirror upstream's unauthenticated
	// /p2p/seeds bootstrap endpoint: the same list principal peers upload to a
	// bootstrap position, in the plain genSeedStr form and the peers-array
	// JSON form the seedlist endpoints already serve.
	PathP2PSeeds       = "/p2p/seeds"
	PathP2PSeedsJSON   = "/p2p/seeds.json"
	PathCrawlReceipt   = "/yacy/crawlReceipt.html"
	PathYaCySearchHTML = "/yacysearch.html"
	PathYaCySearchJSON = "/yacysearch.json"
	PathYaCySearchRSS  = "/yacysearch.rss"
	PathOpenSearch     = "/opensearchdescription.xml"
	PathSuggestJSON    = "/suggest.json"
	PathSuggestXML     = "/suggest.xml"
)

const (
	EndpointMethodsGetPost = EndpointMethodGet | EndpointMethodPost
	EndpointMethodsPost    = EndpointMethodPost
)

const (
	HelloEndpointMethods        = EndpointMethodsGetPost
	TransferRWIEndpointMethods  = EndpointMethodsPost
	TransferURLEndpointMethods  = EndpointMethodsPost
	SearchEndpointMethods       = EndpointMethodsGetPost
	QueryEndpointMethods        = EndpointMethodsGetPost
	IndexEndpointMethods        = EndpointMethodsGetPost
	ListEndpointMethods         = EndpointMethodsGetPost
	MessageEndpointMethods      = EndpointMethodsGetPost
	ProfileEndpointMethods      = EndpointMethodsGetPost
	CrawlURLEndpointMethods     = EndpointMethodsGetPost
	SeedlistEndpointMethods     = EndpointMethodsGetPost
	CrawlReceiptEndpointMethods = EndpointMethodsPost
)
