package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
)

func TestAuthEventObserverRecordsSecurityEvents(t *testing.T) {
	recorder := events.NewRecorder(16)
	observer := authEventObserver{recorder: recorder}

	observer.LoginSucceeded()
	observer.LoginFailed()
	observer.LoginThrottled()
	observer.APIKeyRejected()
	observer.APIKeyThrottled()
	observer.APIKeyForbidden()

	recent := recorder.Recent(0)
	if len(recent) != 6 {
		t.Fatalf("events = %d, want 6", len(recent))
	}
	for _, event := range recent {
		if event.Category != events.CategorySecurity {
			t.Fatalf("category = %q, want security", event.Category)
		}
	}
	if recent[0].Name != "admin.apikey.forbidden" || recent[0].Severity != events.SeverityWarn {
		t.Fatalf("newest event = %#v", recent[0])
	}
	if recent[5].Name != "admin.login.succeeded" || recent[5].Severity != events.SeverityInfo {
		t.Fatalf("oldest event = %#v", recent[5])
	}
}

type fanoutCounter struct {
	loginSucceeded int
	loginFailed    int
	loginThrottled int
	keyRejected    int
	keyThrottled   int
	keyForbidden   int
}

func (c *fanoutCounter) LoginSucceeded()  { c.loginSucceeded++ }
func (c *fanoutCounter) LoginFailed()     { c.loginFailed++ }
func (c *fanoutCounter) LoginThrottled()  { c.loginThrottled++ }
func (c *fanoutCounter) APIKeyRejected()  { c.keyRejected++ }
func (c *fanoutCounter) APIKeyThrottled() { c.keyThrottled++ }
func (c *fanoutCounter) APIKeyForbidden() { c.keyForbidden++ }

func TestAuthObserverFanOutDispatchesToAll(t *testing.T) {
	first := &fanoutCounter{}
	second := &fanoutCounter{}
	fan := authObserverFanOut{first, second}

	fan.LoginSucceeded()
	fan.LoginFailed()
	fan.LoginThrottled()
	fan.APIKeyRejected()
	fan.APIKeyThrottled()
	fan.APIKeyForbidden()

	for _, counter := range []*fanoutCounter{first, second} {
		if *counter != (fanoutCounter{1, 1, 1, 1, 1, 1}) {
			t.Fatalf("counter = %#v, want all ones", counter)
		}
	}
}
