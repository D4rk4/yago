package adminauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

func useFastCredentialWork(t *testing.T) {
	t.Helper()
	originalHash := credentialPasswordHash
	originalVerify := credentialPasswordVerify
	credentialPasswordHash = func(password string) (string, error) {
		return "hash:" + password, nil
	}
	credentialPasswordVerify = func(encoded, password string) (bool, error) {
		return encoded == "hash:"+password, nil
	}
	t.Cleanup(func() {
		credentialPasswordHash = originalHash
		credentialPasswordVerify = originalVerify
	})
}

func occupyCredentialWork(t *testing.T) {
	t.Helper()
	if len(credentialWorkSlots) != 0 {
		t.Fatalf("credential slots already occupied = %d", len(credentialWorkSlots))
	}
	for range maximumConcurrentCredentialWork {
		credentialWorkSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for len(credentialWorkSlots) > 0 {
			<-credentialWorkSlots
		}
	})
}

func TestCredentialWorkSaturationRejectsAuthBeforeArgon(t *testing.T) {
	useFastCredentialWork(t)
	loginService, engine := scriptedService(t)
	injectRawAdmin(t, engine, "admin", "hash:password")
	setupService := testService(t)
	occupyCredentialWork(t)

	responses := []struct {
		recorder *httptest.ResponseRecorder
		name     string
	}{
		{
			doRequest(
				mountAuth(t, loginService),
				http.MethodPost,
				PathLogin,
				`{"username":"admin","password":"password"}`,
			),
			"JSON login",
		},
		{
			postForm(htmlSurface(t, loginService), PathLoginPage, url.Values{
				usernameField: {"admin"}, passwordField: {"password"},
			}),
			"HTML login",
		},
		{
			doRequest(
				mountAuth(t, setupService),
				http.MethodPost,
				PathSetup,
				`{"username":"admin","password":"password"}`,
			),
			"JSON setup",
		},
		{
			postForm(htmlSurface(t, setupService), PathSetupPage, url.Values{
				usernameField: {"admin"}, passwordField: {"password"},
			}),
			"HTML setup",
		},
	}
	for _, response := range responses {
		if response.recorder.Code != http.StatusServiceUnavailable ||
			response.recorder.Header().Get("Retry-After") != "1" {
			t.Fatalf("%s response = %d/%q", response.name,
				response.recorder.Code, response.recorder.Header().Get("Retry-After"))
		}
	}
	if err := loginService.ChangePassword(
		context.Background(),
		"admin",
		"password",
		"new-password",
	); !errors.Is(err, errCredentialWorkUnavailable) {
		t.Fatalf("saturated password change error = %v", err)
	}
	if _, err := setupService.creds.verify(
		context.Background(),
		"missing",
		"password",
	); !errors.Is(err, errCredentialWorkUnavailable) {
		t.Fatalf("saturated dummy verification error = %v", err)
	}

	<-credentialWorkSlots
	if rec := doRequest(
		mountAuth(t, loginService),
		http.MethodPost,
		PathLogin,
		`{"username":"admin","password":"password"}`,
	); rec.Code != http.StatusOK {
		t.Fatalf("released login status = %d", rec.Code)
	}
}

func TestCredentialWorkConcurrentCapacityAndRelease(t *testing.T) {
	originalHash := credentialPasswordHash
	started := make(chan struct{}, maximumConcurrentCredentialWork)
	unblock := make(chan struct{})
	credentialPasswordHash = func(password string) (string, error) {
		started <- struct{}{}
		<-unblock

		return "hash:" + password, nil
	}
	t.Cleanup(func() { credentialPasswordHash = originalHash })

	service := testService(t)
	errorsByWorker := make(chan error, maximumConcurrentCredentialWork)
	for worker := range maximumConcurrentCredentialWork {
		go func() {
			errorsByWorker <- service.creds.setAdmin(
				context.Background(),
				"admin",
				fmt.Sprintf("password-%d", worker),
			)
		}()
	}
	for range maximumConcurrentCredentialWork {
		<-started
	}
	if err := service.creds.setAdmin(
		context.Background(),
		"admin",
		"overflow",
	); !errors.Is(err, errCredentialWorkUnavailable) {
		t.Fatalf("overflow credential work error = %v", err)
	}
	close(unblock)
	for range maximumConcurrentCredentialWork {
		if err := <-errorsByWorker; err != nil {
			t.Fatalf("admitted credential work: %v", err)
		}
	}
	if err := service.creds.setAdmin(
		context.Background(),
		"admin",
		"released",
	); err != nil {
		t.Fatalf("released credential work: %v", err)
	}
}

func TestConcurrentSetupCreatesExactlyOneAdministrator(t *testing.T) {
	useFastCredentialWork(t)
	started := make(chan struct{}, maximumConcurrentCredentialWork)
	unblock := make(chan struct{})
	credentialPasswordHash = func(password string) (string, error) {
		started <- struct{}{}
		<-unblock

		return "hash:" + password, nil
	}
	service := testService(t)
	surface := mountAuth(t, service)
	type setupAttempt struct {
		username string
		password string
		status   int
	}
	start := make(chan struct{})
	attempts := make(chan setupAttempt, 2)
	var requests sync.WaitGroup
	for _, candidate := range []setupAttempt{
		{username: "alice", password: "first"},
		{username: "bob", password: "second"},
	} {
		requests.Add(1)
		go func(candidate setupAttempt) {
			defer requests.Done()
			<-start
			rec := doRequest(
				surface,
				http.MethodPost,
				PathSetup,
				`{"username":"`+candidate.username+`","password":"`+
					candidate.password+`"}`,
			)
			candidate.status = rec.Code
			attempts <- candidate
		}(candidate)
	}
	close(start)
	for range maximumConcurrentCredentialWork {
		<-started
	}
	close(unblock)
	requests.Wait()
	close(attempts)

	var winner, loser setupAttempt
	for attempt := range attempts {
		switch attempt.status {
		case http.StatusCreated:
			winner = attempt
		case http.StatusConflict:
			loser = attempt
		default:
			t.Fatalf("setup status = %d", attempt.status)
		}
	}
	if winner.username == "" || loser.username == "" {
		t.Fatalf("setup outcomes = %#v/%#v", winner, loser)
	}
	winnerValid, winnerErr := service.creds.verify(
		context.Background(), winner.username, winner.password,
	)
	loserValid, loserErr := service.creds.verify(
		context.Background(), loser.username, loser.password,
	)
	if winnerErr != nil || loserErr != nil || !winnerValid || loserValid {
		t.Fatalf("stored winner = %v/%v, loser = %v/%v",
			winnerValid, winnerErr, loserValid, loserErr)
	}
}
