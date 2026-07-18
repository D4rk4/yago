package pipeline

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
)

func WithLeaseGrants(grants *crawllease.GrantRegistry) Option {
	return func(pipeline *Pipeline) {
		pipeline.leaseGrants = grants
	}
}

func (p *Pipeline) grantedJobContext(
	ctx context.Context,
	job crawljob.CrawlJob,
) (context.Context, func(), bool) {
	if p.leaseGrants == nil {
		return ctx, func() {}, true
	}
	grantContext, granted := p.leaseGrants.Context(job.LeaseID)
	if !granted {
		return nil, func() {}, false
	}

	return leaseBoundJobContext(ctx, grantContext, job.LeaseID)
}

func leaseBoundJobContext(
	ctx context.Context,
	grantContext context.Context,
	leaseID string,
) (context.Context, func(), bool) {
	jobContext, cancel := context.WithCancelCause(
		crawllease.WithLeaseID(ctx, leaseID),
	)
	stop := context.AfterFunc(grantContext, func() {
		cancel(crawllease.ErrLeaseLost)
	})
	if grantContext.Err() != nil {
		stop()
		cancel(crawllease.ErrLeaseLost)

		return nil, func() {}, false
	}

	return jobContext, func() {
		stop()
		cancel(nil)
	}, true
}

func (p *Pipeline) leaseAvailabilityChanges() <-chan struct{} {
	if p.leaseGrants == nil {
		return nil
	}

	return p.leaseGrants.AvailabilityChanges()
}

type leaseBindingChangeSource interface {
	LeaseBindingChanges() <-chan struct{}
}

func (p *Pipeline) leaseBindingChanges() <-chan struct{} {
	source, available := p.frontier.(leaseBindingChangeSource)
	if !available {
		return nil
	}

	return source.LeaseBindingChanges()
}

func waitForLeaseAdmissionChange(
	ctx context.Context,
	availabilityChanges <-chan struct{},
	bindingChanges <-chan struct{},
) bool {
	if availabilityChanges == nil && bindingChanges == nil {
		return true
	}
	select {
	case <-availabilityChanges:
		return true
	case <-bindingChanges:
		return true
	case <-ctx.Done():
		return false
	}
}
