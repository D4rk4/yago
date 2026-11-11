package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const maximumTrackedCrawlHosts = 4096

type crawlHost struct {
	nextStart time.Time
	active    int
}

type hostAdmission struct {
	mutex   sync.Mutex
	changed chan struct{}
	hosts   map[string]crawlHost
	delay   time.Duration
	maximum int
}

func newHostAdmission(delay time.Duration, maximum int) *hostAdmission {
	return &hostAdmission{
		changed: make(chan struct{}),
		hosts:   make(map[string]crawlHost),
		delay:   delay,
		maximum: maximum,
	}
}

func (admission *hostAdmission) Acquire(
	ctx context.Context,
	hostname string,
) (func(), error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return nil, errors.New("crawler host is invalid")
	}
	for {
		now := time.Now()
		admission.mutex.Lock()
		admission.pruneLocked(now)
		host, found := admission.hosts[hostname]
		if !found && len(admission.hosts) >= maximumTrackedCrawlHosts {
			changed := admission.changed
			admission.mutex.Unlock()
			if !waitForHostAdmission(ctx, changed, time.Second) {
				return nil, ctx.Err()
			}
			continue
		}
		if !found {
			host = crawlHost{}
		}
		if host.active < admission.maximum && !host.nextStart.After(now) {
			host.active++
			host.nextStart = now.Add(admission.delay)
			admission.hosts[hostname] = host
			admission.mutex.Unlock()
			return func() { admission.Release(hostname) }, nil
		}
		changed := admission.changed
		wait := time.Duration(0)
		if host.nextStart.After(now) {
			wait = host.nextStart.Sub(now)
		}
		admission.mutex.Unlock()
		if !waitForHostAdmission(ctx, changed, wait) {
			return nil, ctx.Err()
		}
	}
}

func (admission *hostAdmission) Release(hostname string) {
	admission.mutex.Lock()
	host, found := admission.hosts[hostname]
	if found && host.active > 0 {
		host.active--
		admission.hosts[hostname] = host
		admission.signalLocked()
	}
	admission.mutex.Unlock()
}

func (admission *hostAdmission) pruneLocked(now time.Time) {
	for hostname, host := range admission.hosts {
		if host.active == 0 && !host.nextStart.After(now) {
			delete(admission.hosts, hostname)
		}
	}
}

func (admission *hostAdmission) signalLocked() {
	close(admission.changed)
	admission.changed = make(chan struct{})
}

func waitForHostAdmission(
	ctx context.Context,
	changed <-chan struct{},
	delay time.Duration,
) bool {
	if delay <= 0 {
		select {
		case <-changed:
			return true
		case <-ctx.Done():
			return false
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-changed:
		return true
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
