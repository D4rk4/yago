package yacyproto

// Wire field names shared by more than one /yacy/* endpoint.
const (
	FieldNetworkName = "network.name"
	FieldIam         = "iam"
	FieldYouAre      = "youare"
	FieldKey         = "key"
	FieldMyTime      = "mytime"
	FieldVersion     = "version"
	FieldUptime      = "uptime"
)

// Wire field names of the hello endpoint.
const (
	FieldSeed     = "seed"
	FieldCount    = "count"
	FieldMagicMD5 = "magicmd5"
	FieldYourIP   = "yourip"
	FieldYourType = "yourtype"
	FieldMessage  = "message"
	prefixSeed    = "seed"
)

// Wire field names of the transferRWI endpoint.
const (
	FieldWordCount  = "wordc"
	FieldEntryCount = "entryc"
	FieldIndexes    = "indexes"
	FieldResult     = "result"
	FieldPause      = "pause"
	FieldUnknownURL = "unknownURL"
)

// Wire field names of the transferURL endpoint.
const (
	FieldURLCount = "urlc"
	FieldDouble   = "double"
	prefixURL     = "url"
)

// Wire field names of the search endpoint.
const (
	FieldMySeed           = "myseed"
	FieldQuery            = "query"
	FieldExclude          = "exclude"
	FieldURLs             = "urls"
	FieldTime             = "time"
	FieldMaxDist          = "maxdist"
	FieldPartitions       = "partitions"
	FieldAbstracts        = "abstracts"
	FieldContentDom       = "contentdom"
	FieldStrictContentDom = "strictContentDom"
	FieldTimezoneOffset   = "timezoneOffset"
	FieldLanguage         = "language"
	FieldModifier         = "modifier"
	FieldPrefer           = "prefer"
	FieldFilter           = "filter"
	FieldConstraint       = "constraint"
	FieldProfile          = "profile"
	FieldSiteHost         = "sitehost"
	FieldSiteHash         = "sitehash"
	FieldAuthor           = "author"
	FieldCollection       = "collection"
	FieldFileType         = "filetype"
	FieldProtocol         = "protocol"
	FieldSearchTime       = "searchtime"
	FieldReferences       = "references"
	FieldJoinCount        = "joincount"
	prefixResource        = "resource"
	prefixIndexCount      = "indexcount."
	prefixIndexAbstract   = "indexabstract."
)

// Wire field names of the query endpoint.
const (
	FieldObject   = "object"
	FieldEnv      = "env"
	FieldResponse = "response"
	FieldMagic    = "magic"
)

// Wire field names of the crawlReceipt endpoint.
const (
	FieldReason    = "reason"
	FieldLURLEntry = "lurlEntry"
	FieldDelay     = "delay"
)
