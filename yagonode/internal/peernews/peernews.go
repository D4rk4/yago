// Package peernews keeps the YaCy peer news pool: news records this peer
// publishes to the network and news records that arrive attached to other
// peers' seeds. A published record is offered once per distribution until it
// reaches the YaCy distribution limit, then it rests in the published queue.
package peernews

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	newsTimestampLayout    = "20060102150405"
	categoryMaxLength      = 8
	maximumNewsRecordBytes = 1024

	attributeOriginator  = "ori"
	attributeCategory    = "cat"
	attributeCreated     = "cre"
	attributeReceived    = "rec"
	attributeDistributed = "dis"
	attributeIDOffset    = "#"
)

var ErrBadNewsRecord = fmt.Errorf("bad news record")

type Record struct {
	Originator  yagomodel.Hash
	Created     time.Time
	Received    time.Time
	Category    string
	Distributed int
	Attributes  map[string]string
}

func (r Record) ID() string {
	return r.Created.UTC().Format(newsTimestampLayout) + r.Originator.String()
}

func (r Record) WireForm() string {
	fields := map[string]string{
		attributeOriginator:  r.Originator.String(),
		attributeCategory:    r.Category,
		attributeCreated:     r.Created.UTC().Format(newsTimestampLayout),
		attributeDistributed: strconv.Itoa(r.Distributed),
	}
	if !r.Received.IsZero() {
		fields[attributeReceived] = r.Received.UTC().Format(newsTimestampLayout)
	}
	for key, value := range r.Attributes {
		if _, standard := fields[key]; !standard {
			fields[key] = value
		}
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(fields[key])
	}
	b.WriteByte('}')

	return b.String()
}

func ParseRecord(wire string, now func() time.Time) (Record, error) {
	return parseRecord(wire, now())
}

func parseRecord(wire string, receivedFallback time.Time) (Record, error) {
	if len(wire) == 0 || len(wire) > maximumNewsRecordBytes {
		return Record{}, fmt.Errorf("%w: record size %d", ErrBadNewsRecord, len(wire))
	}
	fields := parseWireFields(wire)

	originator, err := yagomodel.ParseHash(fields[attributeOriginator])
	if err != nil {
		return Record{}, fmt.Errorf("%w: originator: %w", ErrBadNewsRecord, err)
	}
	category := fields[attributeCategory]
	if len(category) > categoryMaxLength {
		return Record{}, fmt.Errorf("%w: category %q too long", ErrBadNewsRecord, category)
	}
	distributed := 0
	if raw, ok := fields[attributeDistributed]; ok {
		distributed, err = strconv.Atoi(raw)
		if err != nil {
			return Record{}, fmt.Errorf("%w: distributed: %w", ErrBadNewsRecord, err)
		}
	}

	created, err := exactNewsTime(fields[attributeCreated])
	if err != nil {
		return Record{}, fmt.Errorf("%w: created: %w", ErrBadNewsRecord, err)
	}
	record := Record{
		Originator:  originator,
		Created:     created,
		Category:    category,
		Distributed: distributed,
		Attributes:  map[string]string{},
	}
	if raw, ok := fields[attributeReceived]; ok {
		record.Received = newsTime(raw, receivedFallback)
	} else {
		record.Received = receivedFallback
	}
	for key, value := range fields {
		switch key {
		case attributeOriginator, attributeCategory, attributeCreated,
			attributeReceived, attributeDistributed, attributeIDOffset:
		default:
			record.Attributes[key] = value
		}
	}

	return record, nil
}

func exactNewsTime(raw string) (time.Time, error) {
	parsed, err := time.ParseInLocation(newsTimestampLayout, raw, time.UTC)
	if err != nil || parsed.Format(newsTimestampLayout) != raw {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", raw)
	}

	return parsed, nil
}

func parseWireFields(wire string) map[string]string {
	wire = strings.TrimSpace(wire)
	wire = strings.TrimPrefix(wire, "{")
	wire = strings.TrimSuffix(wire, "}")

	fields := map[string]string{}
	for pair := range strings.SplitSeq(wire, ",") {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fields[key] = strings.TrimSpace(value)
	}

	return fields
}

func newsTime(raw string, fallback time.Time) time.Time {
	parsed, err := time.ParseInLocation(newsTimestampLayout, raw, time.UTC)
	if err != nil {
		return fallback.UTC().Truncate(time.Second)
	}

	return parsed
}
