package yagonode

import (
	"github.com/D4rk4/yago/yacynode/internal/adminauth"
	"github.com/D4rk4/yago/yacynode/internal/events"
)

type authEventObserver struct {
	recorder *events.Recorder
}

func (o authEventObserver) LoginSucceeded() {
	o.record(events.SeverityInfo, "admin.login.succeeded", "admin login succeeded")
}

func (o authEventObserver) LoginFailed() {
	o.record(events.SeverityWarn, "admin.login.failed", "admin login failed")
}

func (o authEventObserver) LoginThrottled() {
	o.record(events.SeverityWarn, "admin.login.throttled", "admin login rate limited")
}

func (o authEventObserver) APIKeyRejected() {
	o.record(events.SeverityWarn, "admin.apikey.rejected", "admin API key rejected")
}

func (o authEventObserver) APIKeyThrottled() {
	o.record(events.SeverityWarn, "admin.apikey.throttled", "admin API key rate limited")
}

func (o authEventObserver) APIKeyForbidden() {
	o.record(
		events.SeverityWarn,
		"admin.apikey.forbidden",
		"admin API key lacked the required scope",
	)
}

func (o authEventObserver) record(severity events.Severity, name, message string) {
	o.recorder.Record(severity, events.CategorySecurity, name, message)
}

type authObserverFanOut []adminauth.AuthObserver

func (f authObserverFanOut) LoginSucceeded() {
	for _, observer := range f {
		observer.LoginSucceeded()
	}
}

func (f authObserverFanOut) LoginFailed() {
	for _, observer := range f {
		observer.LoginFailed()
	}
}

func (f authObserverFanOut) LoginThrottled() {
	for _, observer := range f {
		observer.LoginThrottled()
	}
}

func (f authObserverFanOut) APIKeyRejected() {
	for _, observer := range f {
		observer.APIKeyRejected()
	}
}

func (f authObserverFanOut) APIKeyThrottled() {
	for _, observer := range f {
		observer.APIKeyThrottled()
	}
}

func (f authObserverFanOut) APIKeyForbidden() {
	for _, observer := range f {
		observer.APIKeyForbidden()
	}
}
