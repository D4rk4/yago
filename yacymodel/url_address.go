package yacymodel

import (
	"strconv"
	"strings"
)

type urlAddress struct {
	protocol  string
	userInfo  string
	host      string
	port      int
	path      string
	query     string
	hasQuery  bool
	anchor    string
	hasAnchor bool
}

var defaultProtocolPort = map[string]int{
	"http":  80,
	"https": 443,
	"ftp":   21,
	"smb":   445,
}

var sessionIDNames = []string{"phpsessionid", "phpsessid", "jsessionid", "sid"}

func parseURLAddress(raw string) urlAddress {
	a := urlAddress{port: -1, path: "/"}
	proto, rest, ok := strings.Cut(raw, "://")
	if !ok {
		a.protocol = "http"
		a.path = raw
		return a
	}
	a.protocol = strings.ToLower(proto)

	authority := rest
	remainder := ""
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		authority = rest[:i]
		remainder = rest[i:]
	}

	if at := strings.LastIndex(authority, "@"); at >= 0 {
		a.userInfo = authority[:at]
		authority = authority[at+1:]
	}
	a.host, a.port = splitHostPort(authority)
	if a.port < 0 {
		if p, ok := defaultProtocolPort[a.protocol]; ok {
			a.port = p
		}
	}

	if h := strings.IndexByte(remainder, '#'); h >= 0 {
		a.hasAnchor = true
		a.anchor = remainder[h+1:]
		remainder = remainder[:h]
	}
	if q := strings.IndexByte(remainder, '?'); q >= 0 {
		a.hasQuery = true
		a.query = remainder[q+1:]
		remainder = remainder[:q]
	}
	if remainder != "" {
		a.path = remainder
	}
	return a
}

func splitHostPort(authority string) (string, int) {
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return authority, -1
		}
		host := authority[1:end]
		if rest := authority[end+1:]; strings.HasPrefix(rest, ":") {
			if port, err := strconv.Atoi(rest[1:]); err == nil {
				return host, port
			}
		}
		return host, -1
	}
	if c := strings.LastIndex(authority, ":"); c >= 0 {
		if port, err := strconv.Atoi(authority[c+1:]); err == nil {
			return authority[:c], port
		}
	}
	return authority, -1
}

func (a urlAddress) normalform() string {
	defaultPort := false
	switch a.protocol {
	case "http":
		defaultPort = a.port < 0 || a.port == 80
	case "https":
		defaultPort = a.port < 0 || a.port == 443
	case "ftp":
		defaultPort = a.port < 0 || a.port == 21
	case "smb":
		defaultPort = a.port < 0 || a.port == 445
	case "file":
		defaultPort = true
	}

	var b strings.Builder
	b.WriteString(a.protocol)
	b.WriteString("://")
	if a.host != "" {
		if a.userInfo != "" &&
			(a.protocol != "ftp" || !strings.HasPrefix(a.userInfo, "anonymous")) {
			b.WriteString(a.userInfo)
			b.WriteString("@")
		}
		b.WriteString(strings.ToLower(a.host))
	}
	if !defaultPort {
		b.WriteString(":")
		b.WriteString(strconv.Itoa(a.port))
	}
	b.WriteString(a.getFile())
	return b.String()
}

func (a urlAddress) getFile() string {
	if !a.hasQuery {
		return a.path
	}
	q := a.query
	for _, sid := range sessionIDNames {
		lower := strings.ToLower(q)
		if strings.HasPrefix(lower, sid+"=") {
			amp := strings.IndexByte(q, '&')
			if amp < 0 {
				return a.path
			}
			q = q[amp+1:]
			continue
		}
		if p := strings.Index(lower, "&"+sid+"="); p >= 0 {
			if p1 := strings.IndexByte(q[p+1:], '&'); p1 < 0 {
				q = q[:p]
			} else {
				q = q[:p] + q[p+1+p1:]
			}
		}
	}
	return a.path + "?" + q
}

func (a urlAddress) domSubdom() (dom, subdom string) {
	host := a.host
	p := -1
	if host != "" && !strings.Contains(host, ":") {
		p = strings.LastIndex(host, ".")
	}
	if p > 0 {
		dom = host[:p]
	}
	p = strings.LastIndex(dom, ".")
	if p <= 0 {
		return dom, ""
	}
	return dom[p+1:], dom[:p]
}

func (a urlAddress) rootpath() string {
	np := a.path
	if a.protocol == "file" && strings.Contains(np, "\\") {
		np = strings.ReplaceAll(np, "\\", "/")
	}
	rootpathStart := 0
	rootpathEnd := len(np) - 1
	if len(np) > 0 && np[0] == '/' {
		rootpathStart = 1
	}
	if strings.HasSuffix(np, "/") {
		rootpathEnd = len(np) - 2
	}
	p := strings.IndexByte(np[rootpathStart:], '/')
	if p >= 0 {
		p += rootpathStart
	}
	if p > 0 && p < rootpathEnd {
		return np[rootpathStart:p]
	}
	return ""
}
