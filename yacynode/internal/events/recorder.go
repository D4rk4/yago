package events

import (
	"sync"
	"time"
)

const DefaultCapacity = 256

type Recorder struct {
	mu     sync.Mutex
	buffer []Event
	next   int
	filled bool
	now    func() time.Time
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
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buffer[r.next] = Event{
		Time:     r.now().UTC(),
		Severity: severity,
		Category: category,
		Name:     name,
		Message:  message,
	}
	r.next = (r.next + 1) % len(r.buffer)
	if r.next == 0 {
		r.filled = true
	}
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
