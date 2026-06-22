//go:build e2e

package e2e

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

func seedlistSeniorHashes(body []byte) (map[string]struct{}, error) {
	var doc struct {
		Seeds []struct {
			Hash     string `xml:"Hash"`
			PeerType string `xml:"PeerType"`
		} `xml:"seed"`
	}
	out := map[string]struct{}{}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return out, err
	}
	for _, seed := range doc.Seeds {
		if seed.Hash != "" && seed.PeerType == "senior" {
			out[seed.Hash] = struct{}{}
		}
	}
	return out, nil
}

func networkActivePeerHashes(body []byte) (map[string]struct{}, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	out := map[string]struct{}{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return map[string]struct{}{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "hash" {
			continue
		}
		var hash string
		if err := decoder.DecodeElement(&hash, &start); err != nil {
			return map[string]struct{}{}, err
		}
		hash = strings.TrimSpace(hash)
		if hash != "" {
			out[hash] = struct{}{}
		}
	}
	return out, nil
}

func queryResponseCount(body string) (int, bool) {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "response="); ok {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}
