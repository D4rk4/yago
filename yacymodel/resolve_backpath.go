package yacymodel

import "strings"

func resolveBackpath(path string) string {
	p := path
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	for {
		next := collapseBackpath(p)
		if next == p {
			break
		}
		p = next
	}
	for strings.HasPrefix(p, "/../") {
		p = p[3:]
	}
	if p == "/.." {
		p = "/"
	}
	if p == "" {
		return "/"
	}
	return p
}

func collapseBackpath(p string) string {
	var b strings.Builder
	i := 0
	for i < len(p) {
		if p[i] == '/' {
			if seg, end, ok := matchParentBackpath(p, i); ok {
				_ = seg
				i = end
				continue
			}
			if i+2 < len(p) && p[i+1] == '.' && p[i+2] == '/' {
				i += 2
				continue
			}
			if i+1 < len(p) && p[i+1] == '/' {
				i++
				continue
			}
		}
		b.WriteByte(p[i])
		i++
	}
	return b.String()
}

func matchParentBackpath(p string, i int) (string, int, bool) {
	j := i + 1
	for j < len(p) && p[j] != '/' {
		j++
	}
	seg := p[i+1 : j]
	if seg == "" || seg == "." || seg == ".." {
		return "", 0, false
	}
	if j+2 >= len(p) || p[j] != '/' || p[j+1] != '.' || p[j+2] != '.' {
		return "", 0, false
	}
	end := j + 3
	if end == len(p) || p[end] == '/' {
		return seg, end, true
	}
	return "", 0, false
}
