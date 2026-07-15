package adminui

// Search activity — YaCy's AccessTracker_p parity (UI-16). The section shows
// what the node's own query-log privacy mode permits: nothing when logging is
// off, shapes (lengths, counts, latency) in aggregate mode, query text and
// top words in full mode.

import "context"

// ActivityEntry is one recorded search as rendered in the activity table.
// Query is empty outside full mode.
type ActivityEntry struct {
	Time         string
	Query        string
	Length       int
	Terms        int
	Results      int
	ResultsKnown bool
	Complete     bool
	Duration     string
	Source       string
}

// ActivityWord is one row of the top-words tally (full mode only).
type ActivityWord struct {
	Word  string
	Count int
}

// ActivityView is the Search-activity snapshot.
type ActivityView struct {
	// Mode is the effective privacy mode: off, aggregate, or full.
	Mode                 string
	Total                uint64
	ConfirmedZeroResults uint64
	Entries              []ActivityEntry
	TopWords             []ActivityWord
}

// ActivitySource supplies the activity snapshot; nil hides the section body.
type ActivitySource interface {
	Activity(ctx context.Context) ActivityView
}
