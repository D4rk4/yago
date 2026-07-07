//go:build e2e

// Package peerdirectory parses YaCy's peer-directory XML views (seedlist,
// network) into hash sets.
package peerdirectory

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

func SeniorHashes(body []byte) (map[string]struct{}, error) {
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

func ActivePeerHashes(body []byte) (map[string]struct{}, error) {
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
