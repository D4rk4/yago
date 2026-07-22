package peernews

import "time"

func decodeQueuedNewsRecord(wire string) (Record, bool) {
	record, err := parseRecord(wire, time.Time{})

	return record, err == nil
}
