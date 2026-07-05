package adminui

import (
	"context"
	"html/template"
	"strconv"
	"strings"
	"time"
)

// Overview is the node-status snapshot the overview section renders. Its fields
// are primitives so the console stays decoupled from the node's seed schema.
type Overview struct {
	PeerName      string
	PeerHash      string
	PeerType      string
	Version       string
	UptimeSeconds int
	Documents     int
	Words         int
	KnownPeers    int
	SentWords     int64
	ReceivedWords int64
	SentURLs      int64
	ReceivedURLs  int64
}

// OverviewSource supplies the live overview snapshot on each request.
type OverviewSource interface {
	Overview(ctx context.Context) Overview
}

var overviewFuncs = template.FuncMap{"dur": humanDuration}

func humanDuration(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}

	total := time.Duration(seconds) * time.Second
	days := int(total / (24 * time.Hour))
	hours := int(total/time.Hour) % 24
	minutes := int(total/time.Minute) % 60
	secs := int(total/time.Second) % 60

	// Seconds are always shown as the smallest unit so the uptime advances on
	// every console refresh instead of appearing frozen between minute ticks; a
	// larger unit pulls in every unit below it.
	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, strconv.Itoa(days)+"d")
	}
	if days > 0 || hours > 0 {
		parts = append(parts, strconv.Itoa(hours)+"h")
	}
	if days > 0 || hours > 0 || minutes > 0 {
		parts = append(parts, strconv.Itoa(minutes)+"m")
	}
	parts = append(parts, strconv.Itoa(secs)+"s")

	return strings.Join(parts, " ")
}
