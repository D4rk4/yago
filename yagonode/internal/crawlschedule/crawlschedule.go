// Package crawlschedule stores recurring crawl definitions and decides when
// each is due — YaCy's Automation_p (recorded API calls with a recurring
// schedule) parity for the one action worth repeating: re-crawling a site
// (UI-19). Schedules persist in the vault; a background loop re-dispatches a
// due schedule through the same path the console's crawl-start form uses.
package crawlschedule

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// MinInterval keeps schedules polite: re-crawling a site more than hourly is
// abusive for the target and pointless for freshness.
const MinInterval = time.Hour

const scheduleBucket = "crawl_schedules"

// Schedule is one recurring crawl definition.
type Schedule struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Seeds    []string      `json:"seeds"`
	Scope    string        `json:"scope"`
	MaxDepth int           `json:"maxDepth"`
	Interval time.Duration `json:"interval"`
	Enabled  bool          `json:"enabled"`
	LastRun  time.Time     `json:"lastRun"`
}

// Due reports whether the schedule should dispatch now.
func (s Schedule) Due(now time.Time) bool {
	return s.Enabled && now.Sub(s.LastRun) >= s.Interval
}

type scheduleCodec struct{}

func (scheduleCodec) Encode(s Schedule) ([]byte, error) {
	data, _ := json.Marshal(s)

	return data, nil
}

func (scheduleCodec) Decode(raw []byte) (Schedule, error) {
	var s Schedule
	if err := json.Unmarshal(raw, &s); err != nil {
		return Schedule{}, fmt.Errorf("decode crawl schedule: %w", err)
	}

	return s, nil
}

// Store persists schedules in the vault.
type Store struct {
	vault   *vault.Vault
	records *vault.Collection[Schedule]
	now     func() time.Time
}

// Open registers the schedule collection.
func Open(v *vault.Vault, now func() time.Time) (*Store, error) {
	records, err := vault.Register(v, scheduleBucket, scheduleCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl schedules: %w", err)
	}

	return &Store{vault: v, records: records, now: now}, nil
}

// Create validates and stores a new schedule, deriving its ID from the name.
func (s *Store) Create(ctx context.Context, schedule Schedule) (Schedule, error) {
	schedule.Name = strings.TrimSpace(schedule.Name)
	schedule.Seeds = cleanSeeds(schedule.Seeds)
	if schedule.Name == "" {
		return Schedule{}, fmt.Errorf("schedule name is empty")
	}
	if len(schedule.Seeds) == 0 {
		return Schedule{}, fmt.Errorf("schedule has no seed URLs")
	}
	if schedule.Interval < MinInterval {
		return Schedule{}, fmt.Errorf("interval must be at least %s", MinInterval)
	}
	schedule.ID = scheduleID(schedule.Name)
	schedule.Enabled = true
	schedule.LastRun = time.Time{}
	if err := s.put(ctx, schedule); err != nil {
		return Schedule{}, err
	}

	return schedule, nil
}

// List returns every schedule ordered by name.
func (s *Store) List(ctx context.Context) ([]Schedule, error) {
	var schedules []Schedule
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(_ vault.Key, schedule Schedule) (bool, error) {
			schedules = append(schedules, schedule)

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("view crawl schedules: %w", err)
	}
	sort.Slice(schedules, func(i, j int) bool { return schedules[i].Name < schedules[j].Name })

	return schedules, nil
}

// Delete removes one schedule; missing IDs are not an error.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := s.records.Delete(tx, vault.Key(id)); err != nil {
			return fmt.Errorf("delete crawl schedule: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update crawl schedules: %w", err)
	}

	return nil
}

// SetEnabled flips one schedule's enabled flag.
func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) error {
	return s.mutate(ctx, id, func(schedule *Schedule) { schedule.Enabled = enabled })
}

// MarkRan stamps a dispatch so the schedule waits a full interval again.
func (s *Store) MarkRan(ctx context.Context, id string, ranAt time.Time) error {
	return s.mutate(ctx, id, func(schedule *Schedule) { schedule.LastRun = ranAt.UTC() })
}

// DueSchedules lists the schedules that should dispatch now.
func (s *Store) DueSchedules(ctx context.Context) ([]Schedule, error) {
	schedules, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	due := make([]Schedule, 0, len(schedules))
	for _, schedule := range schedules {
		if schedule.Due(now) {
			due = append(due, schedule)
		}
	}

	return due, nil
}

func (s *Store) put(ctx context.Context, schedule Schedule) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.records.Put(tx, vault.Key(schedule.ID), schedule); err != nil {
			return fmt.Errorf("store crawl schedule: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update crawl schedules: %w", err)
	}

	return nil
}

func (s *Store) mutate(ctx context.Context, id string, apply func(*Schedule)) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		schedule, found, err := s.records.Get(tx, vault.Key(id))
		if err != nil {
			return fmt.Errorf("read crawl schedule: %w", err)
		}
		if !found {
			return fmt.Errorf("no such schedule %q", id)
		}
		apply(&schedule)
		if err := s.records.Put(tx, vault.Key(id), schedule); err != nil {
			return fmt.Errorf("store crawl schedule: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update crawl schedules: %w", err)
	}

	return nil
}

// cleanSeeds trims and drops empty seed lines.
func cleanSeeds(seeds []string) []string {
	cleaned := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if seed = strings.TrimSpace(seed); seed != "" {
			cleaned = append(cleaned, seed)
		}
	}

	return cleaned
}

// scheduleID derives a stable key from the name so re-creating a schedule
// with the same name replaces it instead of piling up duplicates.
func scheduleID(name string) string {
	id := strings.ToLower(strings.TrimSpace(name))
	id = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, id)

	return strings.Trim(id, "-")
}
