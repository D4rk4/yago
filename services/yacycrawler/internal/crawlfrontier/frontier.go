package crawlfrontier

import (
	"container/heap"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageadmission"
)

type Entry struct {
	URL       string
	Depth     int
	Deferrals int
	Attempts  int
	NotBefore time.Time
}

type Frontier struct {
	admission *pageadmission.ProfileAdmission
	ready     []Entry
	deferred  deferrals
}

func New(admission *pageadmission.ProfileAdmission) *Frontier {
	return &Frontier{admission: admission}
}

func (f *Frontier) Admit(url string, depth int) bool {
	if !f.admission.Admit(url, depth) {
		return false
	}
	f.ready = append(f.ready, Entry{URL: url, Depth: depth})
	return true
}

func (f *Frontier) HasReady() bool {
	return len(f.ready) > 0
}

func (f *Frontier) Peek() (Entry, bool) {
	if len(f.ready) == 0 {
		return Entry{}, false
	}
	return f.ready[0], true
}

func (f *Frontier) Next() (Entry, bool) {
	if len(f.ready) == 0 {
		return Entry{}, false
	}
	entry := f.ready[0]
	f.ready = f.ready[1:]
	return entry, true
}

func (f *Frontier) Defer(entry Entry) {
	heap.Push(&f.deferred, entry)
}

func (f *Frontier) EarliestDue() (time.Time, bool) {
	if len(f.deferred) == 0 {
		return time.Time{}, false
	}
	return f.deferred[0].NotBefore, true
}

func (f *Frontier) PromoteDue(now time.Time) {
	for len(f.deferred) > 0 && !f.deferred[0].NotBefore.After(now) {
		f.ready = append(f.ready, heap.Pop(&f.deferred).(Entry))
	}
}

func (f *Frontier) Drained() bool {
	return len(f.ready) == 0 && len(f.deferred) == 0
}

func (f *Frontier) DrainPending() int {
	pending := len(f.ready) + len(f.deferred)
	f.ready = nil
	f.deferred = nil
	return pending
}
