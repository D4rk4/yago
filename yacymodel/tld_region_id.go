package yacymodel

import (
	"regexp"
	"strings"
)

const (
	tldEuropeRussiaID        = 0
	tldMiddleSouthAmericaID  = 1
	tldSouthEastAsiaID       = 2
	tldMiddleEastWestAsiaID  = 3
	tldNorthAmericaOceaniaID = 4
	tldAfricaID              = 5
	tldGenericID             = 6
	tldLocalID               = 7
)

var tldRegionGroups = []struct {
	id   int
	tlds []string
}{
	{tldEuropeRussiaID, []string{
		"ad", "al", "aq", "at", "ax", "ba", "be", "bg", "bv", "by", "cat", "ch",
		"cs", "cz", "cy", "de", "dk", "es", "ee", "eu", "fi", "fo", "fr", "fx",
		"gb", "gg", "gi", "gl", "gr", "hr", "hu", "ie", "im", "is", "it", "je",
		"li", "lt", "lu", "lv", "mc", "md", "me", "mk", "mn", "ms", "mt", "mq",
		"nato", "nl", "no", "pf", "pl", "pm", "pt", "ro", "rs", "ru", "se", "si",
		"sj", "sm", "sk", "su", "tf", "uk", "ua", "va", "yu",
	}},
	{tldMiddleSouthAmericaID, []string{
		"ar", "aw", "br", "bo", "cl", "co", "cr", "cu", "do", "ec", "fk", "gf",
		"gt", "gy", "hn", "jm", "mx", "ni", "pa", "pe", "py", "sr", "sv", "uy",
		"ve",
	}},
	{tldSouthEastAsiaID, []string{
		"asia", "bd", "bn", "bt", "cn", "hk", "id", "in", "la", "np", "jp", "kh",
		"kp", "kr", "lk", "my", "mm", "mo", "mv", "ph", "sg", "tp", "th", "tl",
		"tw", "vn",
	}},
	{tldMiddleEastWestAsiaID, []string{
		"ae", "af", "am", "az", "bh", "ge", "il", "iq", "ir", "jo", "kg", "kz",
		"kw", "lb", "ps", "om", "qa", "sa", "sy", "tj", "tm", "pk", "tr", "uz",
		"ye",
	}},
	{tldNorthAmericaOceaniaID, []string{
		"edu", "gov", "mil", "net", "org", "an", "as", "ag", "ai", "au", "bb",
		"bz", "bm", "bs", "ca", "cc", "ck", "cx", "dm", "fm", "fj", "gd", "gp",
		"gs", "gu", "hm", "ht", "io", "ki", "kn", "ky", "lc", "mf", "mh", "mp",
		"nc", "nf", "nr", "nu", "nz", "pg", "pn", "pr", "pw", "sb", "tc", "tk",
		"to", "tt", "tv", "um", "us", "vc", "vg", "vi", "vu", "wf", "ws",
	}},
	{tldAfricaID, []string{
		"ac", "ao", "bf", "bi", "bj", "bw", "cd", "cf", "cg", "ci", "cm", "cv",
		"dj", "dz", "eg", "eh", "er", "et", "ga", "gh", "gm", "gn", "gq", "gw",
		"ke", "km", "lr", "ls", "ly", "ma", "mg", "ml", "mr", "mu", "mw", "mz",
		"na", "ne", "ng", "re", "rw", "sc", "sd", "sh", "sl", "sn", "so", "st",
		"sz", "td", "tg", "tn", "tz", "ug", "za", "zm", "zr", "zw", "yt",
	}},
}

var tldRegionID = buildTLDRegionID()

func buildTLDRegionID() map[string]int {
	m := make(map[string]int)
	for _, group := range tldRegionGroups {
		for _, tld := range group.tlds {
			m[tld] = group.id
		}
	}
	return m
}

var intranetHostPattern = regexp.MustCompile(`(?i)^((localhost)` +
	`|(127\..*)` +
	`|(10\..*)` +
	`|(172\.(1[6-9]|2[0-9]|3[0-1])\..*)` +
	`|(169\.254\..*)` +
	`|(192\.168\..*)` +
	`|(\[?fe80:.*)` +
	`|(\[?0:0:0:0:0:0:0:1.*)` +
	`|(\[?::1)` +
	`|(\[?(fc|fd).*:.*))$`)

func DomainID(host string) int {
	if host == "" {
		return tldLocalID
	}
	tld := ""
	if p := strings.LastIndex(host, "."); p > 0 {
		tld = host[p+1:]
	}
	if id, ok := tldRegionID[tld]; ok {
		return id
	}
	if isLocalHost(host) {
		return tldLocalID
	}
	return tldGenericID
}

func isLocalHost(host string) bool {
	if host == "" {
		return true
	}
	if intranetHostPattern.MatchString(host) {
		return true
	}
	return !strings.Contains(host, ".")
}
