package peerroster

import "context"

func (r *roster) acquireMembership(ctx context.Context) bool {
	if context.Cause(ctx) != nil {
		return false
	}
	select {
	case r.membershipPermit <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *roster) releaseMembership() {
	<-r.membershipPermit
}
