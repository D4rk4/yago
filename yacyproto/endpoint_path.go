package yacyproto

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
	PathCrawlReceipt = "/yacy/crawlReceipt.html"
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
