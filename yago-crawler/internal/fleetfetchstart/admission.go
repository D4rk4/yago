package fleetfetchstart

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	defaultMaximumLeasePermits = yagocrawlcontract.DefaultFetchWorkerConcurrency
	leaseRequestTimeout        = time.Second
	leaseRetryWait             = 100 * time.Millisecond
)

type LeaseClient interface {
	LeaseFetchStarts(
		context.Context,
		*crawlrpc.FetchStartLeaseRequest,
		...grpc.CallOption,
	) (*crawlrpc.FetchStartLeaseDecision, error)
}

type Admission struct {
	operation              chan struct{}
	waiting                atomic.Int64
	client                 LeaseClient
	workerID               string
	workerSessionID        string
	session                *SessionGate
	pagesPerSecond         func() uint32
	permitCapacity         func() int
	upstreamDemand         func() int
	now                    func() time.Time
	wait                   func(context.Context, time.Duration, <-chan struct{}) error
	sessionGeneration      uint64
	sequence               uint64
	sequenceMaximumPermits uint32
	completedSequence      *uint64
	lease                  localFetchStartLease
}

type localFetchStartLease struct {
	sequence       uint64
	permits        uint32
	used           uint32
	firstOpensAt   time.Time
	firstClosesAt  time.Time
	permitInterval time.Duration
	unlimited      bool
}

type AdmissionConfig struct {
	Client          LeaseClient
	WorkerID        string
	WorkerSessionID string
	Session         *SessionGate
	PagesPerSecond  func() uint32
	PermitCapacity  func() int
	UpstreamDemand  func() int
}

func NewAdmission(config AdmissionConfig) *Admission {
	return &Admission{
		operation:       make(chan struct{}, 1),
		client:          config.Client,
		workerID:        config.WorkerID,
		workerSessionID: config.WorkerSessionID,
		session:         config.Session,
		pagesPerSecond:  config.PagesPerSecond,
		permitCapacity:  config.PermitCapacity,
		upstreamDemand:  config.UpstreamDemand,
		now:             time.Now,
		wait:            waitForFleetFetchStart,
		sequence:        1,
	}
}

func (admission *Admission) Wait(ctx context.Context) error {
	admission.waiting.Add(1)
	defer admission.waiting.Add(-1)
	release, err := admission.acquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	return admission.waitForPermit(ctx)
}

func (admission *Admission) acquireOperation(ctx context.Context) (func(), error) {
	select {
	case admission.operation <- struct{}{}:
		return func() { <-admission.operation }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for serialized fleet fetch admission: %w", ctx.Err())
	}
}

func (admission *Admission) waitForPermit(ctx context.Context) error {
	for {
		connected, generation, changed := admission.session.Snapshot()
		if !connected {
			if err := admission.wait(ctx, 0, changed); err != nil {
				return err
			}

			continue
		}
		if admission.sessionGeneration != generation {
			admission.resetForSession(generation)
		}
		if admission.lease.sequence != 0 {
			if admitted, err := admission.usePermit(ctx, changed); admitted || err != nil {
				return err
			}

			continue
		}
		if err := admission.acquireLease(ctx, generation, changed); err != nil {
			if errors.Is(err, errSessionChanged) {
				continue
			}

			return err
		}
	}
}

func (admission *Admission) usePermit(
	ctx context.Context,
	changed <-chan struct{},
) (bool, error) {
	if admission.lease.used >= admission.lease.permits {
		admission.finishLease()

		return false, nil
	}
	if admission.lease.unlimited {
		admission.lease.used++

		return true, nil
	}
	for admission.lease.used < admission.lease.permits {
		permitOffset := time.Duration(admission.lease.used) * admission.lease.permitInterval
		opensAt := admission.lease.firstOpensAt.Add(permitOffset)
		closesAt := admission.lease.firstClosesAt.Add(permitOffset)
		now := admission.now()
		if !now.Before(closesAt) {
			admission.lease.used++

			continue
		}
		if now.Before(opensAt) {
			if err := admission.wait(ctx, opensAt.Sub(now), changed); err != nil {
				return false, err
			}

			return false, nil
		}
		admission.lease.used++

		return true, nil
	}
	admission.finishLease()

	return false, nil
}

func (admission *Admission) acquireLease(
	ctx context.Context,
	generation uint64,
	changed <-chan struct{},
) error {
	if admission.sequenceMaximumPermits == 0 {
		admission.sequenceMaximumPermits = admission.currentMaximumPermits()
	}
	maximumPermits := admission.sequenceMaximumPermits
	request := &crawlrpc.FetchStartLeaseRequest{
		WorkerId:          admission.workerID,
		WorkerSessionId:   admission.workerSessionID,
		Sequence:          admission.sequence,
		MaximumPermits:    maximumPermits,
		CompletedSequence: admission.completedSequence,
	}
	requestStartedAt := admission.now()
	requestContext, cancelRequest := context.WithTimeout(ctx, leaseRequestTimeout)
	response, err := admission.client.LeaseFetchStarts(requestContext, request)
	cancelRequest()
	responseReceivedAt := admission.now()
	connected, currentGeneration, _ := admission.session.Snapshot()
	if !connected || currentGeneration != generation {
		return errSessionChanged
	}
	if err != nil {
		if status.Code(err) == codes.Unimplemented && admission.currentPagesPerSecond() == 0 {
			admission.lease = localFetchStartLease{
				sequence:  admission.sequence,
				permits:   maximumPermits,
				unlimited: true,
			}

			return nil
		}
		if status.Code(err) == codes.InvalidArgument {
			return fmt.Errorf("lease fleet fetch starts: %w", err)
		}
		if waitErr := admission.wait(ctx, leaseRetryWait, changed); waitErr != nil {
			return waitErr
		}

		return nil
	}
	if err := admission.applyDecision(
		requestStartedAt,
		responseReceivedAt,
		maximumPermits,
		response,
	); err != nil {
		return err
	}
	if response.GetGranted() {
		return nil
	}
	retryAfter := time.Duration(response.GetRetryAfterNanoseconds())
	if retryAfter <= 0 {
		return fmt.Errorf("lease fleet fetch starts: invalid retry delay")
	}

	return admission.wait(ctx, retryAfter, changed)
}

func (admission *Admission) applyDecision(
	requestStartedAt time.Time,
	responseReceivedAt time.Time,
	maximumPermits uint32,
	response *crawlrpc.FetchStartLeaseDecision,
) error {
	if response == nil || response.GetSequence() != admission.sequence ||
		response.GetPermits() > maximumPermits {
		return fmt.Errorf("lease fleet fetch starts: invalid decision identity")
	}
	if !response.GetGranted() {
		return nil
	}
	if response.GetPermits() == 0 {
		admission.lease = localFetchStartLease{
			sequence: admission.sequence,
		}

		return nil
	}
	if response.GetUnlimited() {
		admission.lease = localFetchStartLease{
			sequence:  admission.sequence,
			permits:   response.GetPermits(),
			unlimited: true,
		}

		return nil
	}
	interval := time.Duration(response.GetPermitIntervalNanoseconds())
	if interval <= 0 || response.GetFirstPermitClosesAfterNanoseconds() <=
		response.GetFirstPermitOpensAfterNanoseconds() {
		return fmt.Errorf("lease fleet fetch starts: invalid permit window")
	}
	lease := localFetchStartLease{
		sequence: admission.sequence,
		permits:  response.GetPermits(),
		firstOpensAt: responseReceivedAt.Add(
			time.Duration(response.GetFirstPermitOpensAfterNanoseconds()),
		),
		firstClosesAt: requestStartedAt.Add(
			time.Duration(response.GetFirstPermitClosesAfterNanoseconds()),
		),
		permitInterval: interval,
	}
	if !lease.firstOpensAt.Before(lease.firstClosesAt) {
		lease.used = lease.permits
	}
	admission.lease = lease

	return nil
}

func (admission *Admission) finishLease() {
	completed := admission.lease.sequence
	admission.completedSequence = &completed
	admission.sequence++
	admission.sequenceMaximumPermits = 0
	admission.lease = localFetchStartLease{}
}

func (admission *Admission) resetForSession(generation uint64) {
	admission.sessionGeneration = generation
	admission.sequence = 1
	admission.sequenceMaximumPermits = 0
	admission.completedSequence = nil
	admission.lease = localFetchStartLease{}
}

func (admission *Admission) currentPagesPerSecond() uint32 {
	if admission.pagesPerSecond == nil {
		return 0
	}

	return admission.pagesPerSecond()
}

func (admission *Admission) currentMaximumPermits() uint32 {
	waiting := admission.waiting.Load()
	if admission.upstreamDemand != nil {
		waiting = max(waiting, int64(admission.upstreamDemand()))
	}
	if waiting < 1 {
		waiting = 1
	}
	permits := int(defaultMaximumLeasePermits)
	if admission.permitCapacity != nil {
		permits = admission.permitCapacity()
	}
	if permits < 1 {
		permits = 1
	}
	if permits > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		permits = yagocrawlcontract.MaximumFetchWorkerConcurrency
	}
	if waiting < int64(permits) {
		return uint32(waiting)
	}

	return uint32(permits)
}

var errSessionChanged = errors.New("fleet fetch-start session changed")

func waitForFleetFetchStart(
	ctx context.Context,
	wait time.Duration,
	changed <-chan struct{},
) error {
	if wait <= 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for fleet fetch-start change: %w", ctx.Err())
		case <-changed:
			return nil
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for fleet fetch-start permit: %w", ctx.Err())
	case <-changed:
		return nil
	case <-timer.C:
		return nil
	}
}
