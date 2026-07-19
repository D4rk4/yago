package compatibility

import "github.com/D4rk4/yago/yagoproto"

const defaultYaCyAuthenticationScope = "The default freeworld and same-name network path is implemented. Controlled private networks may select salted-magic-sim with one nonempty shared secret for outbound signing and inbound validation."

var yacyPeerSurfaceSpecs = []surfaceSpec{
	{
		Name:    "Peer liveness handshake",
		Path:    yagoproto.PathHello,
		Methods: methods(yagoproto.HelloEndpointMethods),
		State:   Implemented,
		Behavior: "Returns caller IP, caller peer type, own seed, and bounded known seed list after " +
			"back-ping classification, while rejecting self-pings and callers using this peer hash.",
		Evidence: []string{
			"yagonode/internal/peeradmission/*_test.go",
			"yagonode/test/fixtures/yacywire/hello-*",
		},
	},
	{
		Name:    "RWI and URL count query",
		Path:    yagoproto.PathQuery,
		Methods: methods(yagoproto.QueryEndpointMethods),
		State:   Implemented,
		Behavior: "Answers YaCy-compatible rwicount, per-word rwiurlcount, lurlcount, and " +
			"zero-valued wanted* status probes with target identity checks.",
		Evidence: []string{
			"yagonode/internal/nodestatus/*_test.go",
			"yagonode/test/fixtures/yacywire/query-*",
		},
	},
	{
		Name:    "Inbound RWI transfer",
		Path:    yagoproto.PathTransferRWI,
		Methods: methods(yagoproto.TransferRWIEndpointMethods),
		State:   Implemented,
		Behavior: "Checks the YaCy network name and required transfer fields before intake, accepts at most 1,000 RWI postings " +
			"durably before acknowledgement, reports missing URL metadata, returns HTTP 200 too high load when the admission gate is saturated, " +
			"and returns HTTP 200 busy with a millisecond pause for an oversized transfer or pre-commit storage and context pressure.",
		Evidence: []string{
			"yagonode/internal/rwi/*_test.go",
			"yagonode/test/fixtures/yacywire/transfer-rwi-*",
		},
		Notes: defaultYaCyAuthenticationScope,
	},
	{
		Name:    "Inbound URL metadata transfer",
		Path:    yagoproto.PathTransferURL,
		Methods: methods(yagoproto.TransferURLEndpointMethods),
		State:   Implemented,
		Behavior: "Checks the YaCy network name before target handling, accepts at most 1,000 URL metadata rows, reconciles current-process unknown RWI " +
			"references without synchronous lookup reads, and returns HTTP 200 YaCy transferURL backpressure before an uncommitted timeout can become a transport failure.",
		Evidence: []string{
			"yagonode/internal/urlmeta/*_test.go",
			"yagonode/test/fixtures/yacywire/transfer-url-*",
		},
		Notes: defaultYaCyAuthenticationScope,
	},
	{
		Name:    "Remote RWI search",
		Path:    yagoproto.PathSearch,
		Methods: methods(yagoproto.SearchEndpointMethods),
		State:   Implemented,
		Behavior: "Serves key-value YaCy remote search responses with count, joincount, resource rows carrying a transient fixed-order wi WordReferenceRow, " +
			"indexcount, and indexabstract metadata from local RWI storage; term hashes are capped at 32 and URL hashes at 128.",
		Evidence: []string{
			"yagonode/internal/documentsearch/*_test.go",
			"yagonode/test/fixtures/yacywire/search-*",
			"yagonode/test/e2e/interop_matrix_e2e_test.go",
		},
		Notes: defaultYaCyAuthenticationScope,
	},
	{
		Name:    "Plain seed list",
		Path:    yagoproto.PathSeedlist,
		Methods: methods(yagoproto.SeedlistEndpointMethods),
		State:   Implemented,
		Behavior: "Serves own and confirmed reachable seeds in plain YaCy seed-list form with upstream filters, including minversion; " +
			"configured bootstrap import accepts seed UTC offset and timestamp wire values.",
		Evidence: []string{"yagonode/internal/seedlist/*_test.go"},
	},
	{
		Name:     "JSON seed list",
		Path:     yagoproto.PathSeedlistJSON,
		Methods:  methods(yagoproto.SeedlistEndpointMethods),
		State:    Implemented,
		Behavior: "Serves own and confirmed reachable seeds in JSON seed-list form with the same YaCy request filters as the plain seed list.",
		Evidence: []string{"yagonode/internal/seedlist/*_test.go"},
	},
	{
		Name:     "XML seed list",
		Path:     yagoproto.PathSeedlistXML,
		Methods:  methods(yagoproto.SeedlistEndpointMethods),
		State:    Implemented,
		Behavior: "Serves own and confirmed reachable seeds in XML seed-list form with the same YaCy request filters as the plain seed list.",
		Evidence: []string{"yagonode/internal/seedlist/*_test.go"},
	},
	{
		Name:    "Bootstrap seeds",
		Path:    yagoproto.PathP2PSeeds,
		Methods: methods(yagoproto.SeedlistEndpointMethods),
		State:   Implemented,
		Behavior: "Serves the plain CRLF seed-string list at upstream's unauthenticated /p2p/seeds bootstrap path " +
			"with the shared seedlist filters.",
		Evidence: []string{"yagonode/internal/seedlist/*_test.go"},
	},
	{
		Name:    "Bootstrap seeds JSON",
		Path:    yagoproto.PathP2PSeedsJSON,
		Methods: methods(yagoproto.SeedlistEndpointMethods),
		State:   Implemented,
		Behavior: "Serves the peers-array JSON bootstrap shape (hash-first seed maps plus public Address entries, " +
			"JSONP callback supported) from the same backend as the JSON seed list.",
		Evidence: []string{"yagonode/internal/seedlist/*_test.go"},
	},
	{
		Name:    "Host-link index",
		Path:    yagoproto.PathIndex,
		Methods: methods(yagoproto.IndexEndpointMethods),
		State:   Implemented,
		Behavior: "Serves the YaCy idx.json host object shape with a bounded incoming host-link index counted from stored document " +
			"outlinks per source host, advertising the exact hostReferenceRow rowdef and emitting each reference in YaCy's " +
			"toPropertyForm(':') shape ({\"h\":hash,\"m\":decimal-day,\"c\":decimal-count}).",
		Evidence: []string{
			"yagonode/internal/hostlinks/*_test.go",
			"yagonode/internal/urlmeta/*_test.go",
			"yagonode/internal/yagonode/host_link_source_test.go",
			"yagoproto/index_test.go",
		},
		Notes: "object=host is upstream idx.java's only implemented object; verified against " +
			"source/net/yacy/htroot/yacy/idx.java and WebStructureGraph.java (2026-07).",
	},
	{
		Name:     "Shared blacklist export",
		Path:     yagoproto.PathList,
		Methods:  methods(yagoproto.ListEndpointMethods),
		State:    Implemented,
		Behavior: "Checks the YaCy network unit and serves shared blacklist col=black responses from files named in YAGO_DATA_DIR/SETTINGS/yacy.conf BlackLists.Shared under YAGO_DATA_DIR/LISTS, honouring the listname filter and stripping comment lines.",
		Evidence: []string{
			"yagonode/internal/sharedblacklist/*_test.go",
			"yagonode/internal/yagonode/node_shared_blacklist_test.go",
			"yagoproto/list_test.go",
		},
		Notes: "col=black is upstream list.java's only implemented column; verified against source/net/yacy/htroot/yacy/list.java (2026-07).",
	},
	{
		Name:     "Peer message inbox",
		Path:     yagoproto.PathMessage,
		Methods:  methods(yagoproto.MessageEndpointMethods),
		State:    Implemented,
		Behavior: "Serves the youare-addressed permission handshake and stores post-only peer message posts, with the subject and body decoded from the wire form, into a durable inbox.",
		Evidence: []string{"yagonode/internal/peermessage/*_test.go", "yagoproto/message_test.go"},
		Notes:    "Full parity with upstream message.java, which itself advertises attachmentsize 0, stores no attachments, and comments out the iam requirement (verified against source/net/yacy/htroot/yacy/message.java, 2026-07).",
	},
	{
		Name:     "Peer profile export",
		Path:     yagoproto.PathProfile,
		Methods:  methods(yagoproto.ProfileEndpointMethods),
		State:    Implemented,
		Behavior: "Serves the YaCy profile text shape (key=value lines, \\r stripped and \\n escaped, empty pairs dropped) with properties loaded from YAGO_DATA_DIR/SETTINGS/profile.txt as Java properties.",
		Evidence: []string{
			"yagonode/internal/peerprofile/*_test.go",
			"yagonode/internal/yagonode/node_profile_test.go",
			"yagoproto/profile_test.go",
		},
		Notes: "A missing profile.txt yields an empty profile in upstream too (profile.java swallows the read error); verified against source/net/yacy/htroot/yacy/profile.java (2026-07).",
	},
	{
		Name:     "Remote crawl URL feed",
		Path:     yagoproto.PathCrawlURLs,
		Methods:  methods(yagoproto.CrawlURLEndpointMethods),
		State:    Partial,
		Behavior: "Serves URL-hash metadata feeds and an empty remote-crawl feed by default. A complete salted-magic-sim, trusted-peer, and destination-allowlist policy leases at most 100 durable single-URL items per request, with bounded per-peer rate, outstanding leases, expiry, and pending requeue across restart.",
		Evidence: []string{
			"yagonode/internal/crawlurls/*_test.go",
			"yagonode/internal/remotecrawl/*_test.go",
			"yagonode/internal/yagonode/node_remote_crawl*_test.go",
			"yagoproto/crawl_urls_test.go",
		},
		Notes: "Default-deny delegation copies only locally accepted URL orders, never removes local work, and remains intentionally narrower than Java YaCy's unrestricted local crawler coupling.",
	},
	{
		Name:     "Remote crawl receipt",
		Path:     yagoproto.PathCrawlReceipt,
		Methods:  methods(yagoproto.CrawlReceiptEndpointMethods),
		State:    Partial,
		Behavior: "Accepts the bounded YaCy crawl receipt shape. Disabled, authentication, malformed, mismatched, expired, and valid non-fill outcomes return delay 3600; destination-policy rejection returns 9999; an unexpired trusted peer-and-URL-matching fill stores bounded URL metadata, removes the lease, and returns 10. A receipt cannot create or extend work.",
		Evidence: []string{
			"yagonode/internal/crawling/*_test.go",
			"yagonode/internal/remotecrawl/*_test.go",
			"yagoproto/crawl_receipt_test.go",
		},
		Notes: "Enabled receipts require salted-magic-sim, a nonempty secret, the exact trusted leasing peer, and destination revalidation; remote bodies, profiles, redirects, and follow-up depth are not delegated.",
	},
}

func methods(set yagoproto.EndpointMethodSet) []string {
	var out []string
	if set&yagoproto.EndpointMethodGet != 0 {
		out = append(out, "GET")
	}
	if set&yagoproto.EndpointMethodPost != 0 {
		out = append(out, "POST")
	}

	return out
}
