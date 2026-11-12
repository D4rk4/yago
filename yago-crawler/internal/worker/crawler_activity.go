package worker

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maximumCrawlerActivityHosts       = 64
	maximumCrawlerActivityHostsLogged = 8
)

type crawlerRequestKind uint8

const (
	crawlerPageRequest crawlerRequestKind = iota
	crawlerRobotsRequest
)

type crawlerRequestActivity struct {
	started              uint64
	completed            uint64
	failed               uint64
	admissionFailures    uint64
	responses2xx         uint64
	responses3xx         uint64
	responses4xx         uint64
	responses5xx         uint64
	responsesOther       uint64
	admissionWaitSamples uint64
	admissionWaitTotal   time.Duration
	admissionWaitMaximum time.Duration
	requestTimeTotal     time.Duration
	requestTimeMaximum   time.Duration
}

type crawlerHostActivity struct {
	Hostname                string `json:"hostname"`
	PageRequestsStarted     uint64 `json:"pageRequestsStarted"`
	PageRequestsCompleted   uint64 `json:"pageRequestsCompleted"`
	PageRequestFailures     uint64 `json:"pageRequestFailures"`
	RobotsRequestsStarted   uint64 `json:"robotsRequestsStarted"`
	RobotsRequestsCompleted uint64 `json:"robotsRequestsCompleted"`
	RobotsRequestFailures   uint64 `json:"robotsRequestFailures"`
	AdmissionWaitTotalMs    uint64 `json:"admissionWaitTotalMs"`
	AdmissionWaitSamples    uint64 `json:"admissionWaitSamples"`
	RequestTimeTotalMs      uint64 `json:"requestTimeTotalMs"`
}

type crawlerActivityWindow struct {
	Elapsed               time.Duration
	Page                  crawlerRequestActivity
	Robots                crawlerRequestActivity
	UntrackedHostRequests uint64
	OmittedHostSummaries  uint64
	Hosts                 []crawlerHostActivity
	IngestMutationsSent   uint64
	IngestSendFailures    uint64
	NodeAcksAccepted      uint64
	NodeAcksRejected      uint64
	NodeAckFailures       uint64
	NodeAckTimeTotal      time.Duration
	NodeAckTimeMaximum    time.Duration
}

type crawlerActivity struct {
	mutex                 sync.Mutex
	windowStarted         time.Time
	page                  crawlerRequestActivity
	robots                crawlerRequestActivity
	hosts                 map[string]crawlerHostActivity
	untrackedHostRequests uint64
	ingestMutationsSent   uint64
	ingestSendFailures    uint64
	nodeAcksAccepted      uint64
	nodeAcksRejected      uint64
	nodeAckFailures       uint64
	nodeAckTimeTotal      time.Duration
	nodeAckTimeMaximum    time.Duration
}

func newCrawlerActivity(now time.Time) *crawlerActivity {
	return &crawlerActivity{
		windowStarted: now,
		hosts:         make(map[string]crawlerHostActivity),
	}
}

func (activity *crawlerActivity) recordHostAdmission(
	kind crawlerRequestKind,
	hostname string,
	wait time.Duration,
	admitted bool,
) {
	if activity == nil {
		return
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	activity.mutex.Lock()
	defer activity.mutex.Unlock()
	request := activity.request(kind)
	request.admissionWaitSamples++
	request.admissionWaitTotal += wait
	if wait > request.admissionWaitMaximum {
		request.admissionWaitMaximum = wait
	}
	if !admitted {
		request.admissionFailures++
	}
	if hostname == "" {
		return
	}
	host, found := activity.hosts[hostname]
	if !found {
		if len(activity.hosts) >= maximumCrawlerActivityHosts {
			if admitted {
				activity.untrackedHostRequests++
			}
			return
		}
		host.Hostname = hostname
	}
	if admitted {
		request.started++
		if kind == crawlerPageRequest {
			host.PageRequestsStarted++
		} else {
			host.RobotsRequestsStarted++
		}
	}
	host.AdmissionWaitTotalMs += durationMilliseconds(wait)
	host.AdmissionWaitSamples++
	activity.hosts[hostname] = host
}

func (activity *crawlerActivity) recordHTTPRequestCompleted(
	kind crawlerRequestKind,
	hostname string,
	elapsed time.Duration,
	status int,
	failed bool,
) {
	if activity == nil {
		return
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	activity.mutex.Lock()
	defer activity.mutex.Unlock()
	request := activity.request(kind)
	request.completed++
	request.requestTimeTotal += elapsed
	if elapsed > request.requestTimeMaximum {
		request.requestTimeMaximum = elapsed
	}
	if failed {
		request.failed++
	}
	switch {
	case status >= 200 && status < 300:
		request.responses2xx++
	case status >= 300 && status < 400:
		request.responses3xx++
	case status >= 400 && status < 500:
		request.responses4xx++
	case status >= 500 && status < 600:
		request.responses5xx++
	case status != 0:
		request.responsesOther++
	}
	if host, found := activity.hosts[hostname]; found {
		if kind == crawlerPageRequest {
			host.PageRequestsCompleted++
			if failed {
				host.PageRequestFailures++
			}
		} else {
			host.RobotsRequestsCompleted++
			if failed {
				host.RobotsRequestFailures++
			}
		}
		host.RequestTimeTotalMs += durationMilliseconds(elapsed)
		activity.hosts[hostname] = host
	}
}

func (activity *crawlerActivity) recordIngestSend(failed bool) {
	if activity == nil {
		return
	}
	activity.mutex.Lock()
	if failed {
		activity.ingestSendFailures++
	} else {
		activity.ingestMutationsSent++
	}
	activity.mutex.Unlock()
}

func (activity *crawlerActivity) recordNodeAcknowledgment(
	elapsed time.Duration,
	errorCode int32,
	rejected bool,
) {
	if activity == nil {
		return
	}
	activity.mutex.Lock()
	defer activity.mutex.Unlock()
	activity.nodeAckTimeTotal += elapsed
	if elapsed > activity.nodeAckTimeMaximum {
		activity.nodeAckTimeMaximum = elapsed
	}
	switch {
	case errorCode != 0:
		activity.nodeAckFailures++
	case rejected:
		activity.nodeAcksRejected++
	default:
		activity.nodeAcksAccepted++
	}
}

func (activity *crawlerActivity) takeWindow(now time.Time) crawlerActivityWindow {
	if activity == nil {
		return crawlerActivityWindow{Elapsed: time.Minute}
	}
	activity.mutex.Lock()
	defer activity.mutex.Unlock()
	window := crawlerActivityWindow{
		Elapsed:               now.Sub(activity.windowStarted),
		Page:                  activity.page,
		Robots:                activity.robots,
		UntrackedHostRequests: activity.untrackedHostRequests,
		IngestMutationsSent:   activity.ingestMutationsSent,
		IngestSendFailures:    activity.ingestSendFailures,
		NodeAcksAccepted:      activity.nodeAcksAccepted,
		NodeAcksRejected:      activity.nodeAcksRejected,
		NodeAckFailures:       activity.nodeAckFailures,
		NodeAckTimeTotal:      activity.nodeAckTimeTotal,
		NodeAckTimeMaximum:    activity.nodeAckTimeMaximum,
	}
	for _, host := range activity.hosts {
		window.Hosts = append(window.Hosts, host)
	}
	sort.Slice(window.Hosts, func(left, right int) bool {
		leftRequests := window.Hosts[left].PageRequestsStarted + window.Hosts[left].RobotsRequestsStarted
		rightRequests := window.Hosts[right].PageRequestsStarted + window.Hosts[right].RobotsRequestsStarted
		if leftRequests == rightRequests {
			return window.Hosts[left].Hostname < window.Hosts[right].Hostname
		}
		return leftRequests > rightRequests
	})
	if len(window.Hosts) > maximumCrawlerActivityHostsLogged {
		window.OmittedHostSummaries = uint64(len(window.Hosts) - maximumCrawlerActivityHostsLogged)
		window.Hosts = window.Hosts[:maximumCrawlerActivityHostsLogged]
	}
	activity.windowStarted = now
	activity.page = crawlerRequestActivity{}
	activity.robots = crawlerRequestActivity{}
	activity.hosts = make(map[string]crawlerHostActivity)
	activity.untrackedHostRequests = 0
	activity.ingestMutationsSent = 0
	activity.ingestSendFailures = 0
	activity.nodeAcksAccepted = 0
	activity.nodeAcksRejected = 0
	activity.nodeAckFailures = 0
	activity.nodeAckTimeTotal = 0
	activity.nodeAckTimeMaximum = 0
	return window
}

func (activity *crawlerActivity) request(kind crawlerRequestKind) *crawlerRequestActivity {
	if kind == crawlerRobotsRequest {
		return &activity.robots
	}
	return &activity.page
}

func durationMilliseconds(duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}
	return uint64(duration / time.Millisecond)
}
