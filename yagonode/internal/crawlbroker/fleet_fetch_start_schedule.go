package crawlbroker

import (
	"container/list"
	"errors"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	defaultFleetFetchReservationHorizon = 250 * time.Millisecond
	defaultFleetFetchLeaseLifetime      = time.Second
)

var (
	errFleetFetchPolicyInvalid  = errors.New("fleet fetch-start policy is invalid")
	errFleetFetchRequestInvalid = errors.New("fleet fetch-start lease request is invalid")
	errFleetFetchSessionActive  = errors.New(
		"fleet fetch-start worker session is already active",
	)
	errFleetFetchSessionStale          = errors.New("fleet fetch-start worker session is stale")
	errFleetFetchLeaseOutstanding      = errors.New("fleet fetch-start lease is still outstanding")
	errFleetFetchSequenceStale         = errors.New("fleet fetch-start lease sequence is stale")
	errFleetFetchLeaseNotFound         = errors.New("fleet fetch-start lease was not found")
	errFleetFetchLeaseSequenceMismatch = errors.New(
		"fleet fetch-start lease sequence does not match",
	)
	errFleetFetchCapabilityRequired = errors.New("fetch-start lease capability is required")
)

type fleetFetchStartPolicy struct {
	PagesPerSecond      uint32
	MaximumLeasePermits uint32
	ReservationHorizon  time.Duration
	LeaseLifetime       time.Duration
	RestartQuietPeriod  time.Duration
}

type fleetFetchStartLeaseRequest struct {
	WorkerID        string
	WorkerSessionID string
	Sequence        uint64
	MaximumPermits  uint32
}

type fleetFetchStartLease struct {
	WorkerID          string
	WorkerSessionID   string
	Sequence          uint64
	Permits           uint32
	FirstPermitAt     time.Time
	PermitInterval    time.Duration
	PermitStartWindow time.Duration
	ExpiresAt         time.Time
	PolicyGeneration  uint64
	Unlimited         bool
}

type fleetFetchStartDecision struct {
	Lease            fleetFetchStartLease
	Granted          bool
	RetryAt          time.Time
	ServerObservedAt time.Time
}

type fleetFetchStartPermitWindow struct {
	OpensAt  time.Time
	ClosesAt time.Time
}

type relativeFleetFetchStartPermitWindow struct {
	OpensAfter  time.Duration
	ClosesAfter time.Duration
}

type fleetFetchStartSnapshot struct {
	PagesPerSecond        uint32
	PolicyGeneration      uint64
	ActiveSessionTotal    int
	OutstandingLeaseTotal int
	WaitingSessionTotal   int
	NextPermitAt          time.Time
	QuietUntil            time.Time
}

type fleetFetchSessionIdentity struct {
	workerID        string
	workerSessionID string
}

type fleetFetchStartLeaseRecord struct {
	lease       fleetFetchStartLease
	outstanding bool
}

type pendingFleetFetchStartRequest struct {
	request fleetFetchStartLeaseRequest
	entry   *list.Element
}

type fleetFetchStartSchedule struct {
	mutex            sync.Mutex
	policy           fleetFetchStartPolicy
	now              func() time.Time
	policyGeneration uint64
	quietUntil       time.Time
	nextPermitAt     time.Time
	lastPermitAt     time.Time
	activeSessions   map[string]string
	leases           map[fleetFetchSessionIdentity]fleetFetchStartLeaseRecord
	waiting          map[fleetFetchSessionIdentity]*pendingFleetFetchStartRequest
	waitingRotation  *list.List
}

func newFleetFetchStartSchedule(
	pagesPerSecond uint32,
) (*fleetFetchStartSchedule, error) {
	return newFleetFetchStartScheduleAt(fleetFetchStartPolicy{
		PagesPerSecond:      pagesPerSecond,
		MaximumLeasePermits: yagocrawlcontract.MaximumFetchWorkerConcurrency,
		ReservationHorizon:  defaultFleetFetchReservationHorizon,
		LeaseLifetime:       defaultFleetFetchLeaseLifetime,
		RestartQuietPeriod:  defaultFleetFetchLeaseLifetime,
	}, time.Now)
}

func newFleetFetchStartScheduleAt(
	policy fleetFetchStartPolicy,
	now func() time.Time,
) (*fleetFetchStartSchedule, error) {
	if !validFleetFetchStartPolicy(policy) || now == nil {
		return nil, errFleetFetchPolicyInvalid
	}
	startedAt := now()

	return &fleetFetchStartSchedule{
		policy:           policy,
		now:              now,
		policyGeneration: 1,
		quietUntil:       startedAt.Add(policy.RestartQuietPeriod),
		activeSessions:   make(map[string]string),
		leases:           make(map[fleetFetchSessionIdentity]fleetFetchStartLeaseRecord),
		waiting:          make(map[fleetFetchSessionIdentity]*pendingFleetFetchStartRequest),
		waitingRotation:  list.New(),
	}, nil
}

func validFleetFetchStartPolicy(policy fleetFetchStartPolicy) bool {
	return policy.PagesPerSecond <= yagocrawlcontract.MaximumProcessPagesPerSecond &&
		policy.MaximumLeasePermits > 0 &&
		policy.MaximumLeasePermits <= yagocrawlcontract.MaximumFetchWorkerConcurrency &&
		policy.ReservationHorizon > 0 &&
		policy.LeaseLifetime > policy.ReservationHorizon &&
		policy.LeaseLifetime-policy.ReservationHorizon > policy.ReservationHorizon &&
		policy.RestartQuietPeriod >= 0
}

func (schedule *fleetFetchStartSchedule) ActivateSession(
	workerID string,
	workerSessionID string,
) error {
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return errFleetFetchRequestInvalid
	}
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	if activeSessionID, found := schedule.activeSessions[workerID]; found {
		if activeSessionID == workerSessionID {
			return nil
		}

		return errFleetFetchSessionActive
	}
	schedule.activeSessions[workerID] = workerSessionID

	return nil
}

func (schedule *fleetFetchStartSchedule) replaceSession(
	workerID string,
	workerSessionID string,
) {
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	if previousSessionID := schedule.activeSessions[workerID]; previousSessionID != "" &&
		previousSessionID != workerSessionID {
		previousIdentity := fleetFetchSessionIdentity{
			workerID:        workerID,
			workerSessionID: previousSessionID,
		}
		delete(schedule.leases, previousIdentity)
		schedule.removeWaitingLocked(previousIdentity)
	}
	schedule.activeSessions[workerID] = workerSessionID
}

func (schedule *fleetFetchStartSchedule) DeactivateSession(
	workerID string,
	workerSessionID string,
) bool {
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return false
	}
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	if schedule.activeSessions[workerID] != workerSessionID {
		return false
	}
	delete(schedule.activeSessions, workerID)
	identity := fleetFetchSessionIdentity{workerID: workerID, workerSessionID: workerSessionID}
	delete(schedule.leases, identity)
	schedule.removeWaitingLocked(identity)

	return true
}

func (schedule *fleetFetchStartSchedule) Lease(
	request fleetFetchStartLeaseRequest,
) (fleetFetchStartDecision, error) {
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	if !schedule.validRequestLocked(request) {
		return fleetFetchStartDecision{}, errFleetFetchRequestInvalid
	}
	identity := fleetFetchSessionIdentity{
		workerID:        request.WorkerID,
		workerSessionID: request.WorkerSessionID,
	}
	if !schedule.sessionCurrentLocked(identity) {
		return fleetFetchStartDecision{}, errFleetFetchSessionStale
	}
	now := schedule.now()
	if decision, found, err := schedule.existingLeaseLocked(
		identity,
		request.Sequence,
		now,
	); found ||
		err != nil {
		return decision, err
	}
	if waiting, found := schedule.waiting[identity]; found {
		if waiting.request.Sequence != request.Sequence {
			return fleetFetchStartDecision{}, errFleetFetchLeaseOutstanding
		}
	} else {
		waiting := &pendingFleetFetchStartRequest{request: request}
		waiting.entry = schedule.waitingRotation.PushBack(waiting)
		schedule.waiting[identity] = waiting
	}
	schedule.dispatchLocked(now)
	if decision, found, err := schedule.existingLeaseLocked(
		identity,
		request.Sequence,
		now,
	); found ||
		err != nil {
		return decision, err
	}

	return fleetFetchStartDecision{
		Lease: fleetFetchStartLease{
			PolicyGeneration: schedule.policyGeneration,
		},
		RetryAt:          schedule.retryAtLocked(),
		ServerObservedAt: now,
	}, nil
}

func (schedule *fleetFetchStartSchedule) validRequestLocked(
	request fleetFetchStartLeaseRequest,
) bool {
	return validCrawlerLeaseIdentity(request.WorkerID, request.WorkerSessionID) &&
		request.Sequence > 0 && request.MaximumPermits > 0 &&
		request.MaximumPermits <= schedule.policy.MaximumLeasePermits
}

func (schedule *fleetFetchStartSchedule) sessionCurrentLocked(
	identity fleetFetchSessionIdentity,
) bool {
	return schedule.activeSessions[identity.workerID] == identity.workerSessionID
}

func (schedule *fleetFetchStartSchedule) existingLeaseLocked(
	identity fleetFetchSessionIdentity,
	sequence uint64,
	now time.Time,
) (fleetFetchStartDecision, bool, error) {
	record, found := schedule.leases[identity]
	if !found {
		return fleetFetchStartDecision{}, false, nil
	}
	if record.outstanding && !record.lease.ExpiresAt.After(now) {
		record.outstanding = false
		schedule.leases[identity] = record
	}
	if sequence < record.lease.Sequence {
		return fleetFetchStartDecision{}, false, errFleetFetchSequenceStale
	}
	if sequence == record.lease.Sequence {
		return fleetFetchStartDecision{
			Lease:            record.lease,
			Granted:          true,
			ServerObservedAt: now,
		}, true, nil
	}
	if record.outstanding {
		return fleetFetchStartDecision{}, false, errFleetFetchLeaseOutstanding
	}

	return fleetFetchStartDecision{}, false, nil
}

func (schedule *fleetFetchStartSchedule) dispatchLocked(now time.Time) {
	for schedule.waitingRotation.Len() > 0 {
		entry := schedule.waitingRotation.Front()
		waiting := entry.Value.(*pendingFleetFetchStartRequest)
		identity := fleetFetchSessionIdentity{
			workerID:        waiting.request.WorkerID,
			workerSessionID: waiting.request.WorkerSessionID,
		}
		lease, available := schedule.nextLeaseLocked(now, waiting.request)
		if !available {
			return
		}
		schedule.removeWaitingLocked(identity)
		schedule.leases[identity] = fleetFetchStartLeaseRecord{
			lease:       lease,
			outstanding: true,
		}
	}
}

func (schedule *fleetFetchStartSchedule) nextLeaseLocked(
	now time.Time,
	request fleetFetchStartLeaseRequest,
) (fleetFetchStartLease, bool) {
	lease := fleetFetchStartLease{
		WorkerID:         request.WorkerID,
		WorkerSessionID:  request.WorkerSessionID,
		Sequence:         request.Sequence,
		ExpiresAt:        now.Add(schedule.policy.LeaseLifetime),
		PolicyGeneration: schedule.policyGeneration,
	}
	if schedule.policy.PagesPerSecond == 0 {
		lease.Permits = request.MaximumPermits
		lease.Unlimited = true

		return lease, true
	}
	interval := fleetFetchStartInterval(schedule.policy.PagesPerSecond)
	firstPermitAt := now
	if schedule.quietUntil.After(firstPermitAt) {
		firstPermitAt = schedule.quietUntil
	}
	if schedule.nextPermitAt.After(firstPermitAt) {
		firstPermitAt = schedule.nextPermitAt
	}
	latestPermitAt := now.Add(schedule.policy.ReservationHorizon)
	if firstPermitAt.After(latestPermitAt) {
		return fleetFetchStartLease{}, false
	}
	lease.Permits = 1
	for lease.Permits < request.MaximumPermits &&
		!firstPermitAt.Add(time.Duration(lease.Permits)*interval).After(latestPermitAt) {
		lease.Permits++
	}
	lease.FirstPermitAt = firstPermitAt
	lease.PermitInterval = interval
	lease.PermitStartWindow = min(schedule.policy.ReservationHorizon, interval)
	lastPermitAt := firstPermitAt.Add(
		time.Duration(lease.Permits-1) * interval,
	)
	schedule.lastPermitAt = lastPermitAt
	schedule.nextPermitAt = lastPermitAt.Add(interval)

	return lease, true
}

func (lease fleetFetchStartLease) PermitWindow(
	permitOrdinal uint32,
) (fleetFetchStartPermitWindow, bool) {
	if lease.Unlimited || permitOrdinal >= lease.Permits {
		return fleetFetchStartPermitWindow{}, false
	}
	opensAt := lease.FirstPermitAt.Add(time.Duration(permitOrdinal) * lease.PermitInterval)
	closesAt := opensAt.Add(lease.PermitStartWindow)

	return fleetFetchStartPermitWindow{OpensAt: opensAt, ClosesAt: closesAt}, true
}

func (decision fleetFetchStartDecision) RelativePermitWindow(
	permitOrdinal uint32,
) (relativeFleetFetchStartPermitWindow, bool) {
	window, found := decision.Lease.PermitWindow(permitOrdinal)
	if !found || decision.ServerObservedAt.IsZero() ||
		!window.ClosesAt.After(decision.ServerObservedAt) {
		return relativeFleetFetchStartPermitWindow{}, false
	}

	return relativeFleetFetchStartPermitWindow{
		OpensAfter:  window.OpensAt.Sub(decision.ServerObservedAt),
		ClosesAfter: window.ClosesAt.Sub(decision.ServerObservedAt),
	}, true
}

func (schedule *fleetFetchStartSchedule) retryAtLocked() time.Time {
	firstPermitAt := schedule.nextPermitAt
	if schedule.quietUntil.After(firstPermitAt) {
		firstPermitAt = schedule.quietUntil
	}

	return firstPermitAt.Add(-schedule.policy.ReservationHorizon)
}

func (schedule *fleetFetchStartSchedule) CompleteLease(
	workerID string,
	workerSessionID string,
	sequence uint64,
) error {
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) || sequence == 0 {
		return errFleetFetchRequestInvalid
	}
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	identity := fleetFetchSessionIdentity{workerID: workerID, workerSessionID: workerSessionID}
	if !schedule.sessionCurrentLocked(identity) {
		return errFleetFetchSessionStale
	}
	record, found := schedule.leases[identity]
	if !found {
		return errFleetFetchLeaseNotFound
	}
	if record.lease.Sequence < sequence {
		return errFleetFetchLeaseSequenceMismatch
	}
	if record.lease.Sequence > sequence {
		return nil
	}
	record.outstanding = false
	schedule.leases[identity] = record

	return nil
}

func (schedule *fleetFetchStartSchedule) SetPagesPerSecond(
	pagesPerSecond uint32,
) error {
	if pagesPerSecond > yagocrawlcontract.MaximumProcessPagesPerSecond {
		return errFleetFetchPolicyInvalid
	}
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	if schedule.policy.PagesPerSecond == pagesPerSecond {
		return nil
	}
	previousPagesPerSecond := schedule.policy.PagesPerSecond
	schedule.policy.PagesPerSecond = pagesPerSecond
	schedule.policyGeneration++
	now := schedule.now()
	if pagesPerSecond > 0 {
		interval := fleetFetchStartInterval(pagesPerSecond)
		minimumNextPermitAt := now
		if previousPagesPerSecond == 0 {
			minimumNextPermitAt = now.Add(interval)
		} else if !schedule.lastPermitAt.IsZero() {
			minimumNextPermitAt = schedule.lastPermitAt.Add(interval)
		}
		if schedule.nextPermitAt.Before(minimumNextPermitAt) {
			schedule.nextPermitAt = minimumNextPermitAt
		}
	}
	schedule.dispatchLocked(now)

	return nil
}

func fleetFetchStartInterval(pagesPerSecond uint32) time.Duration {
	rate := time.Duration(pagesPerSecond)

	return (time.Second + rate - 1) / rate
}

func (schedule *fleetFetchStartSchedule) Snapshot() fleetFetchStartSnapshot {
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()
	now := schedule.now()
	outstanding := 0
	for _, record := range schedule.leases {
		if record.outstanding && record.lease.ExpiresAt.After(now) {
			outstanding++
		}
	}

	return fleetFetchStartSnapshot{
		PagesPerSecond:        schedule.policy.PagesPerSecond,
		PolicyGeneration:      schedule.policyGeneration,
		ActiveSessionTotal:    len(schedule.activeSessions),
		OutstandingLeaseTotal: outstanding,
		WaitingSessionTotal:   schedule.waitingRotation.Len(),
		NextPermitAt:          schedule.nextPermitAt,
		QuietUntil:            schedule.quietUntil,
	}
}

func (schedule *fleetFetchStartSchedule) removeWaitingLocked(
	identity fleetFetchSessionIdentity,
) {
	waiting, found := schedule.waiting[identity]
	if !found {
		return
	}
	schedule.waitingRotation.Remove(waiting.entry)
	delete(schedule.waiting, identity)
}
