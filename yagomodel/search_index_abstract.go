package yagomodel

import (
	"fmt"
	"slices"
	"strings"
)

func EncodeSearchIndexAbstract(urlHashes []Hash) string {
	if len(urlHashes) == 0 {
		return "{}"
	}

	domains := make(map[string][]string)
	for _, hash := range urlHashes {
		if !hash.Valid() {
			continue
		}
		raw := hash.String()
		domains[raw[6:]] = append(domains[raw[6:]], raw[:6])
	}

	if len(domains) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(domains))
	for domain := range domains {
		keys = append(keys, domain)
	}
	slices.SortFunc(keys, compareBase64Strings)
	for _, domain := range keys {
		slices.SortFunc(domains[domain], compareBase64Strings)
	}

	var b strings.Builder
	b.WriteByte('{')
	for i, domain := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(domain)
		b.WriteByte(':')
		for _, path := range domains[domain] {
			b.WriteString(path)
		}
	}
	b.WriteByte('}')
	return b.String()
}

func DecodeSearchIndexAbstract(raw string) ([]Hash, error) {
	if raw == "" || raw == "{}" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		return nil, fmt.Errorf("%w: index abstract envelope", ErrInvalidHash)
	}

	body := strings.TrimSuffix(strings.TrimPrefix(raw, "{"), "}")
	var hashes []Hash
	for _, group := range strings.Split(body, ",") {
		domain, paths, ok := strings.Cut(group, ":")
		if !ok || len(domain) != hostHashLength || len(paths)%hostHashLength != 0 {
			return nil, fmt.Errorf("%w: index abstract group %q", ErrInvalidHash, group)
		}
		if _, err := ParseHash(domain + domain); err != nil {
			return nil, fmt.Errorf("%w: index abstract host %q", err, domain)
		}
		for start := 0; start < len(paths); start += hostHashLength {
			hash, err := ParseHash(paths[start:start+hostHashLength] + domain)
			if err != nil {
				return nil, fmt.Errorf("%w: index abstract path", err)
			}
			hashes = append(hashes, hash)
		}
	}

	return hashes, nil
}

func compareBase64Strings(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		av := base64Order(a[i])
		bv := base64Order(b[i])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func base64Order(c byte) int {
	for i := range len(Alphabet) {
		if Alphabet[i] == c {
			return i
		}
	}
	return len(Alphabet) + int(c)
}
