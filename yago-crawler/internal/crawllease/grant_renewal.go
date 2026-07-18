package crawllease

import (
	"time"
)

type grantRenewalOutcome uint8

const (
	grantRenewalUnchanged grantRenewalOutcome = iota
	grantRenewalConfirmed
	grantRenewalExtended
	grantRenewalLost
)

func (current *grant) applyRenewal(
	requestStarted time.Time,
	deadline time.Time,
	now time.Time,
	accepted bool,
	timeToLive time.Duration,
) grantRenewalOutcome {
	if current.confirmed && requestStarted.Before(current.renewedAt) {
		return grantRenewalUnchanged
	}
	if current.settling.active() && requestStarted.Before(current.settling.responseAt) {
		return grantRenewalUnchanged
	}
	responseLive := accepted && timeToLive > 0 && deadline.After(now)
	if current.settling.active() {
		current.settling.recordResponse(requestStarted, responseLive)
		if !responseLive {
			return grantRenewalUnchanged
		}
	} else if !responseLive ||
		current.confirmed && !requestStarted.Before(current.expiresAt) {
		return grantRenewalLost
	}
	wasConfirmed := current.confirmed
	current.confirmed = true
	current.renewedAt = requestStarted
	if deadline.After(current.expiresAt) {
		current.expiresAt = deadline
	}
	if !wasConfirmed {
		return grantRenewalConfirmed
	}

	return grantRenewalExtended
}
