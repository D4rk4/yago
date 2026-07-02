package compatibility

import "github.com/D4rk4/yago/yacyproto"

var yacyPeerSurfaceSpecs = []surfaceSpec{
	{
		Name:    "Peer liveness handshake",
		Path:    yacyproto.PathHello,
		Methods: methods(yacyproto.HelloEndpointMethods),
		State:   Implemented,
		Behavior: "Returns caller IP, caller peer type, own seed, and bounded known seed list after " +
			"back-ping classification, while rejecting self-pings and callers using this peer hash.",
		Evidence: []string{
			"yacynode/internal/peeradmission/*_test.go",
			"yacynode/test/fixtures/yacywire/hello-*",
		},
	},
	{
		Name:    "RWI and URL count query",
		Path:    yacyproto.PathQuery,
		Methods: methods(yacyproto.QueryEndpointMethods),
		State:   Implemented,
		Behavior: "Answers YaCy-compatible rwicount, per-word rwiurlcount, lurlcount, and " +
			"zero-valued wanted* status probes with target identity checks.",
		Evidence: []string{
			"yacynode/internal/nodestatus/*_test.go",
			"yacynode/test/fixtures/yacywire/query-*",
		},
	},
	{
		Name:    "Inbound RWI transfer",
		Path:    yacyproto.PathTransferRWI,
		Methods: methods(yacyproto.TransferRWIEndpointMethods),
		State:   Implemented,
		Behavior: "Checks the YaCy network unit and required transfer fields before intake, accepts RWI postings " +
			"durably before acknowledgement, and reports missing URL metadata with YaCy transferRWI fields.",
		Evidence: []string{
			"yacynode/internal/rwi/*_test.go",
			"yacynode/test/fixtures/yacywire/transfer-rwi-*",
		},
	},
	{
		Name:    "Inbound URL metadata transfer",
		Path:    yacyproto.PathTransferURL,
		Methods: methods(yacyproto.TransferURLEndpointMethods),
		State:   Implemented,
		Behavior: "Accepts URL metadata rows, reconciles RWI references, and returns YaCy transferURL " +
			"result fields.",
		Evidence: []string{
			"yacynode/internal/urlmeta/*_test.go",
			"yacynode/test/fixtures/yacywire/transfer-url-*",
		},
	},
	{
		Name:    "Remote RWI search",
		Path:    yacyproto.PathSearch,
		Methods: methods(yacyproto.SearchEndpointMethods),
		State:   Implemented,
		Behavior: "Serves key-value YaCy remote search responses with count, joincount, resource rows, " +
			"indexcount, and indexabstract metadata from local RWI storage.",
		Evidence: []string{
			"yacynode/internal/documentsearch/*_test.go",
			"yacynode/test/fixtures/yacywire/search-*",
		},
	},
	{
		Name:    "Plain seed list",
		Path:    yacyproto.PathSeedlist,
		Methods: methods(yacyproto.SeedlistEndpointMethods),
		State:   Implemented,
		Behavior: "Serves own and confirmed reachable seeds in plain YaCy seed-list form with upstream filters, including minversion; " +
			"configured bootstrap import accepts seed UTC offset and timestamp wire values.",
		Evidence: []string{"yacynode/internal/seedlist/*_test.go"},
	},
	{
		Name:     "JSON seed list",
		Path:     yacyproto.PathSeedlistJSON,
		Methods:  methods(yacyproto.SeedlistEndpointMethods),
		State:    Implemented,
		Behavior: "Serves own and confirmed reachable seeds in JSON seed-list form with the same YaCy request filters as the plain seed list.",
		Evidence: []string{"yacynode/internal/seedlist/*_test.go"},
	},
	{
		Name:     "XML seed list",
		Path:     yacyproto.PathSeedlistXML,
		Methods:  methods(yacyproto.SeedlistEndpointMethods),
		State:    Implemented,
		Behavior: "Serves own and confirmed reachable seeds in XML seed-list form with the same YaCy request filters as the plain seed list.",
		Evidence: []string{"yacynode/internal/seedlist/*_test.go"},
	},
	{
		Name:     "Host-link index",
		Path:     yacyproto.PathIndex,
		Methods:  methods(yacyproto.IndexEndpointMethods),
		State:    Partial,
		Behavior: "Serves the YaCy idx.json host object shape with a bounded incoming host-link index inferred from stored URL metadata referrers.",
		Evidence: []string{
			"yacynode/internal/hostlinks/*_test.go",
			"yacynode/internal/urlmeta/*_test.go",
			"yacynode/cmd/yacy-rwi-node/host_link_source_test.go",
			"yacyproto/index_test.go",
		},
		Notes: "Only object=host is implemented.",
	},
	{
		Name:     "Shared blacklist export",
		Path:     yacyproto.PathList,
		Methods:  methods(yacyproto.ListEndpointMethods),
		State:    Partial,
		Behavior: "Serves YaCy shared blacklist text shape for col=black from files named in YACY_DATA_DIR/SETTINGS/yacy.conf BlackLists.Shared under YACY_DATA_DIR/LISTS.",
		Evidence: []string{
			"yacynode/internal/sharedblacklist/*_test.go",
			"yacynode/cmd/yacy-rwi-node/node_shared_blacklist_test.go",
			"yacyproto/list_test.go",
		},
		Notes: "Only col=black is implemented.",
	},
	{
		Name:     "Peer message inbox",
		Path:     yacyproto.PathMessage,
		Methods:  methods(yacyproto.MessageEndpointMethods),
		State:    Partial,
		Behavior: "Accepts permission checks without parsing post-only body fields and inbound peer message posts into a durable inbox.",
		Evidence: []string{"yacynode/internal/peermessage/*_test.go", "yacyproto/message_test.go"},
		Notes:    "Attachments are advertised as size 0 and are not stored.",
	},
	{
		Name:     "Peer profile export",
		Path:     yacyproto.PathProfile,
		Methods:  methods(yacyproto.ProfileEndpointMethods),
		State:    Partial,
		Behavior: "Serves the YaCy profile text shape with properties loaded from YACY_DATA_DIR/SETTINGS/profile.txt when that file exists.",
		Evidence: []string{
			"yacynode/internal/peerprofile/*_test.go",
			"yacynode/cmd/yacy-rwi-node/node_profile_test.go",
			"yacyproto/profile_test.go",
		},
		Notes: "Missing profile files produce an empty profile.",
	},
	{
		Name:     "Remote crawl URL feed",
		Path:     yacyproto.PathCrawlURLs,
		Methods:  methods(yacyproto.CrawlURLEndpointMethods),
		State:    Partial,
		Behavior: "Serves URL-hash metadata feeds and safe empty remote-crawl feeds while remote crawl execution is disabled.",
		Evidence: []string{"yacynode/internal/crawlurls/*_test.go", "yacyproto/crawl_urls_test.go"},
		Notes:    "Remote crawl work is disabled by default for SSRF safety.",
	},
	{
		Name:     "Remote crawl receipt",
		Path:     yacyproto.PathCrawlReceipt,
		Methods:  methods(yacyproto.CrawlReceiptEndpointMethods),
		State:    Partial,
		Behavior: "Accepts the YaCy crawl receipt wire shape, checks the target peer hash, and returns YaCy's rejected-receipt retry delay while remote crawl execution is disabled.",
		Evidence: []string{
			"yacynode/internal/crawling/*_test.go",
			"yacyproto/crawl_receipt_test.go",
		},
		Notes: "Remote crawl work is disabled by default for SSRF safety.",
	},
}

func methods(set yacyproto.EndpointMethodSet) []string {
	var out []string
	if set&yacyproto.EndpointMethodGet != 0 {
		out = append(out, "GET")
	}
	if set&yacyproto.EndpointMethodPost != 0 {
		out = append(out, "POST")
	}

	return out
}
