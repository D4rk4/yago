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
	CrawlReceiptEndpointMethods = EndpointMethodsPost
)
