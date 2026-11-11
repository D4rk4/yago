package frontier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	bolt "go.etcd.io/bbolt"
)

const maximumHostRateLimitStreak uint32 = 32

func (frontier *Frontier) RecordHostRateLimit(
	ctx context.Context,
	hostname string,
	now time.Time,
	retryAt time.Time,
) (time.Time, error) {
	if err := checkContext(ctx); err != nil {
		return time.Time{}, err
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" || now.IsZero() || retryAt.IsZero() {
		return time.Time{}, errors.New("invalid host rate limit")
	}
	var cooldownUntil time.Time
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		hosts := transaction.Bucket([]byte(hostsBucketName))
		schedule, err := loadHostSchedule(hosts.Get([]byte(hostname)))
		if err != nil {
			return err
		}
		if schedule.RateLimitStreak < maximumHostRateLimitStreak {
			schedule.RateLimitStreak++
		}
		cooldownUntil = now.Add(hostRateLimitBackoff(
			schedule.RateLimitStreak,
			frontier.config.HostDelay,
		))
		if retryAt.After(cooldownUntil) {
			cooldownUntil = retryAt
		}
		if schedule.NextAllowed.After(cooldownUntil) {
			cooldownUntil = schedule.NextAllowed
		}
		schedule.NextAllowed = cooldownUntil.UTC()
		return saveHostSchedule(hosts, hostname, schedule)
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("record host rate limit: %w", err)
	}
	return cooldownUntil, nil
}

func releaseHostClaim(hosts *bolt.Bucket, hostname string, settledState State) error {
	schedule, err := loadHostSchedule(hosts.Get([]byte(hostname)))
	if err != nil {
		return err
	}
	if schedule.Active == 0 {
		return fmt.Errorf("host %s active count underflow", hostname)
	}
	schedule.Active--
	if settledState == StateFetched || settledState == StateIndexed {
		schedule.RateLimitStreak = 0
	}
	return saveHostSchedule(hosts, hostname, schedule)
}

func hostRateLimitBackoff(streak uint32, hostDelay time.Duration) time.Duration {
	backoff := max(hostDelay, time.Second)
	maximum := yagocrawlcontract.MaximumCrawlBackoffDelay
	if backoff > maximum {
		backoff = maximum
	}
	for streak > 1 && backoff < maximum {
		if backoff >= maximum/2 {
			return maximum
		}
		backoff *= 2
		streak--
	}
	return backoff
}
