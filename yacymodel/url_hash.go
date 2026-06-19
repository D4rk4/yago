package yacymodel

import (
	"crypto/md5"
	"strconv"
	"strings"
)

func URLHash(rawURL string) Hash {
	a := parseURLAddress(rawURL)

	dom, subdom := a.domSubdom()
	rootpath := a.rootpath()

	flag := DomainID(a.host)<<2 | int(domLengthKey(len(dom)))
	if a.protocol != "http" {
		flag |= 32
	}

	port := strconv.Itoa(a.port)

	var h strings.Builder
	h.WriteString(md5Base64(a.normalform())[:5])
	h.WriteByte(md5Base64(subdom + ":" + port + ":" + rootpath)[0])
	h.WriteString(md5Base64(a.protocol + ":" + hostForHash(a.host) + ":" + port)[:5])
	h.WriteByte(Alphabet[flag&0x3f])
	return Hash(h.String())
}

func md5Base64(s string) string {
	sum := md5.Sum([]byte(s))
	return Encode(sum[:])
}

func hostForHash(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func domLengthKey(l int) byte {
	switch {
	case l <= 8:
		return 0
	case l <= 12:
		return 1
	case l <= 16:
		return 2
	default:
		return 3
	}
}
