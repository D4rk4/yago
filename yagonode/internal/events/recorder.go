package events

import (
	"sync"
	"time"
)

const DefaultCapacity = 256

// Sink durably persists each recorded event so the log survives a restart. Its
// Persist is called outside the recorder lock and must not block indefinitely.
type Sink interface {
	Persist(Event)
}

type Recorder struct {
	mu     sync.Mutex
	buffer []Event
	next   int
	filled bool
	now    func() time.Time
	sink   Sink
}

func NewRecorder(capacity int) *Recorder {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}

	return &Recorder{
		buffer: make([]Event, capacity),
		now:    time.Now,
	}
}

func (r *Recorder) Record(severity Severity, category Category, name, message string) {
	event := Event{
		Time:     r.now().UTC(),
		Severity: severity,
		Category: category,
		Name:     name,
		Message:  message,
	}

	r.mu.Lock()
	r.buffer[r.next] = event
	r.next = (r.next + 1) % len(r.buffer)
	if r.next == 0 {
		r.filled = true
	}
	sink := r.sink
	r.mu.Unlock()

	if sink != nil {
		sink.Persist(event)
	}
}

// Attach seeds the ring with prior events (oldest first) and installs a durable
// sink that receives every subsequent event. It is called once at startup, so
// the in-memory log begins populated with what survived the last restart.
func (r *Recorder) Attach(sink Sink, history []Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, event := range history {
		r.buffer[r.next] = event
		r.next = (r.next + 1) % len(r.buffer)
		if r.next == 0 {
			r.filled = true
		}
	}
	r.sink = sink
}

func (r *Recorder) Recent(limit int) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := r.next
	if r.filled {
		count = len(r.buffer)
	}
	if limit > 0 && limit < count {
		count = limit
	}

	out := make([]Event, 0, count)
	for i := 0; i < count; i++ {
		idx := (r.next - 1 - i + len(r.buffer)) % len(r.buffer)
		out = append(out, r.buffer[idx])
	}

	return out
}
