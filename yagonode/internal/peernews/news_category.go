package peernews

const (
	CategoryProfileUpdate    = "prfleupd"
	CategoryProfileBroadcast = "prflecst"
	CategoryCrawlStart       = "crwlstrt"
	CategoryBookmarkAdd      = "bkmrkadd"
	CategorySurftippAdd      = "stippadd"
	CategorySurftippVoteAdd  = "stippavt"
	CategoryWikiUpdate       = "wiki_upd"
	CategoryBlogAdd          = "blog_add"
	CategoryTranslationAdd   = "transadd"
)

var knownNewsCategories = map[string]bool{
	CategoryProfileUpdate:    true,
	CategoryProfileBroadcast: true,
	"prflegvt":               true,
	"prflebvt":               true,
	CategoryCrawlStart:       true,
	"crwlstop":               true,
	"crwlcomm":               true,
	"blckladd":               true,
	"blcklavt":               true,
	"blckldel":               true,
	"blckldvt":               true,
	"flshradd":               true,
	"flshrdel":               true,
	"flshrcom":               true,
	CategoryBookmarkAdd:      true,
	"bkmrkavt":               true,
	"bkmrkmov":               true,
	"bkmrkmvt":               true,
	"bkmrkdel":               true,
	"bkmrkdvt":               true,
	CategorySurftippAdd:      true,
	CategorySurftippVoteAdd:  true,
	CategoryWikiUpdate:       true,
	"wiki_del":               true,
	CategoryBlogAdd:          true,
	"blog_del":               true,
	CategoryTranslationAdd:   true,
	"transavt":               true,
}
