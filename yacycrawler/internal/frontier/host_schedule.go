package frontier

import (
	"container/heap"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/weburl"
)

type hostSchedule struct {
	pace    CrawlPace
	queues  map[string][]crawljob.CrawlJob
	entries map[string]*hostDueEntry
	due     hostDueQueue
}

func newHostSchedule(pace CrawlPace) *hostSchedule {
	return &hostSchedule{
		pace:    pace,
		queues:  make(map[string][]crawljob.CrawlJob),
		entries: make(map[string]*hostDueEntry),
	}
}

func (s *hostSchedule) push(job crawljob.CrawlJob, now time.Time) {
	host := weburl.Host(job.URL)
	s.queues[host] = append(s.queues[host], job)
	if _, tracked := s.entries[host]; tracked {
		return
	}
	entry := &hostDueEntry{host: host, due: s.pace.DueAt(job, now)}
	s.entries[host] = entry
	heap.Push(&s.due, entry)
}

func (s *hostSchedule) len() int {
	return len(s.due)
}

func (s *hostSchedule) peek(now time.Time) (crawljob.CrawlJob, time.Duration, bool) {
	if len(s.due) == 0 {
		return crawljob.CrawlJob{}, 0, false
	}
	entry := s.due[0]
	wait := entry.due.Sub(now)
	if wait > 0 {
		return crawljob.CrawlJob{}, wait, false
	}
	return s.queues[entry.host][0], 0, true
}

func (s *hostSchedule) dispatched(job crawljob.CrawlJob, at time.Time) {
	host := weburl.Host(job.URL)
	s.pace.Visited(job, at)
	queue := s.queues[host][1:]
	if len(queue) == 0 {
		delete(s.queues, host)
		heap.Remove(&s.due, s.entries[host].index)
		delete(s.entries, host)
		return
	}
	s.queues[host] = queue
	entry := s.entries[host]
	entry.due = s.pace.DueAt(queue[0], at)
	heap.Fix(&s.due, entry.index)
}

type hostDueEntry struct {
	host  string
	due   time.Time
	index int
}

type hostDueQueue []*hostDueEntry

func (q hostDueQueue) Len() int { return len(q) }

func (q hostDueQueue) Less(i, j int) bool { return q[i].due.Before(q[j].due) }

func (q hostDueQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *hostDueQueue) Push(x any) {
	entry := x.(*hostDueEntry)
	entry.index = len(*q)
	*q = append(*q, entry)
}

func (q *hostDueQueue) Pop() any {
	old := *q
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	*q = old[:n-1]
	return entry
}
