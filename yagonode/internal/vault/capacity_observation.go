package vault

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const capacityObservationLifetime = time.Second

type capacityObservation struct {
	mutex      sync.Mutex
	now        func() time.Time
	used       int64
	observedAt time.Time
	revision   uint64
	exactStart uint64
	exactDone  uint64
	mutation   uint64
	current    *capacityMeasurement
}

type exactCapacityMeasurement struct {
	sequence uint64
	mutation uint64
}

type capacityMeasurement struct {
	done  chan struct{}
	used  int64
	err   error
	retry bool
}

type capacityMeasurementClaim struct {
	measurement *capacityMeasurement
	revision    uint64
	mutation    uint64
	used        int64
	cached      bool
	leads       bool
}

func newCapacityObservation() *capacityObservation {
	return &capacityObservation{now: time.Now}
}

func (o *capacityObservation) beginExactMeasurement() exactCapacityMeasurement {
	o.mutex.Lock()
	o.exactStart++
	measurement := exactCapacityMeasurement{
		sequence: o.exactStart,
		mutation: o.mutation,
	}
	o.mutex.Unlock()

	return measurement
}

func (o *capacityObservation) recordExactMeasurement(
	measurement exactCapacityMeasurement,
	used int64,
) {
	o.mutex.Lock()
	if measurement.sequence < o.exactDone {
		o.mutex.Unlock()

		return
	}
	o.exactDone = measurement.sequence
	if measurement.mutation != o.mutation {
		o.mutex.Unlock()

		return
	}
	o.used = used
	o.observedAt = o.now()
	o.revision++
	o.mutex.Unlock()
}

func (o *capacityObservation) measure(
	ctx context.Context,
	read func(context.Context) (int64, error),
) (int64, error) {
	for {
		claim := o.claimMeasurement()
		if claim.cached {
			return claim.used, nil
		}
		if claim.leads {
			used, err, retry := o.performMeasurement(ctx, read, claim)
			if retry {
				continue
			}

			return used, err
		}
		used, err, retry := awaitMeasurement(ctx, claim.measurement)
		if !retry {
			return used, err
		}
	}
}

func (o *capacityObservation) claimMeasurement() capacityMeasurementClaim {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if used, fresh := o.freshObservationLocked(); fresh {
		return capacityMeasurementClaim{used: used, cached: true}
	}
	if o.current != nil {
		return capacityMeasurementClaim{measurement: o.current}
	}
	measurement := &capacityMeasurement{done: make(chan struct{})}
	o.current = measurement

	return capacityMeasurementClaim{
		measurement: measurement,
		revision:    o.revision,
		mutation:    o.mutation,
		leads:       true,
	}
}

func (o *capacityObservation) freshObservationLocked() (int64, bool) {
	age := o.now().Sub(o.observedAt)
	if o.observedAt.IsZero() || age < 0 || age >= capacityObservationLifetime {
		return 0, false
	}

	return o.used, true
}

func awaitMeasurement(
	ctx context.Context,
	measurement *capacityMeasurement,
) (int64, error, bool) {
	select {
	case <-ctx.Done():
		return 0, fmt.Errorf("wait for capacity observation: %w", ctx.Err()), false
	case <-measurement.done:
	}
	if measurement.retry {
		return 0, nil, true
	}
	if measurement.err == nil {
		return measurement.used, nil, false
	}
	if errors.Is(measurement.err, context.Canceled) ||
		errors.Is(measurement.err, context.DeadlineExceeded) {
		return 0, nil, true
	}

	return 0, measurement.err, false
}

func (o *capacityObservation) performMeasurement(
	ctx context.Context,
	read func(context.Context) (int64, error),
	claim capacityMeasurementClaim,
) (int64, error, bool) {
	used, err := read(ctx)
	if err != nil {
		err = fmt.Errorf("measure capacity observation: %w", err)
	}
	retry := false
	o.mutex.Lock()
	if o.mutation != claim.mutation || o.revision != claim.revision {
		if observed, fresh := o.freshObservationLocked(); fresh {
			used = observed
			err = nil
		} else {
			used = 0
			err = nil
			retry = true
		}
	} else if err == nil {
		o.used = used
		o.observedAt = o.now()
		o.revision++
	}
	claim.measurement.used = used
	claim.measurement.err = err
	claim.measurement.retry = retry
	o.current = nil
	close(claim.measurement.done)
	o.mutex.Unlock()

	return used, err, retry
}

func (o *capacityObservation) recordMutation() {
	o.mutex.Lock()
	o.mutation++
	o.mutex.Unlock()
}
