package clickcapture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sequenceEntropy struct {
	next byte
}

func (r *sequenceEntropy) Read(target []byte) (int, error) {
	for index := range target {
		target[index] = r.next
		r.next++
	}

	return len(target), nil
}

type failingEntropy struct {
	remaining int
}

func (r *failingEntropy) Read(target []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	read := min(len(target), r.remaining)
	for index := range read {
		target[index] = byte(index)
	}
	r.remaining -= read
	if read < len(target) {
		return read, io.ErrUnexpectedEOF
	}

	return read, nil
}

type mutableClock struct {
	unix atomic.Int64
}

func newMutableClock(at time.Time) *mutableClock {
	clock := &mutableClock{}
	clock.unix.Store(at.Unix())

	return clock
}

func (c *mutableClock) Now() time.Time {
	return time.Unix(c.unix.Load(), 0).UTC()
}

func (c *mutableClock) Set(at time.Time) {
	c.unix.Store(at.Unix())
}

func TestIssuerRoundTripNormalizesAndAuthenticates(t *testing.T) {
	issuer, clock := issuerFixture(t)
	results := displayedFixture("https://a.example/", "https://b.example/")
	token, err := issuer.Issue("  Mixed   QUERY ", "model-a", results)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := issuer.parse(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.query != "mixed query" || claims.modelAssignment != "model-a" ||
		len(claims.results) != 2 || claims.issuedAt != clock.Now().Unix() {
		t.Fatalf("claims = %#v", claims)
	}
	click, err := issuer.ValidateClick(token, "https://a.example/", 1)
	if err != nil {
		t.Fatalf("ValidateClick: %v", err)
	}
	if click.Query != "mixed query" || click.ModelAssignment != "model-a" ||
		click.Candidate.ClusterIdentity != "cluster-0" {
		t.Fatalf("click = %#v", click)
	}
}

func TestIssuerRejectsTamperExpiryAndFutureTokens(t *testing.T) {
	issuer, clock := issuerFixture(t)
	token := mustIssueToken(t, issuer, displayedFixture("https://a.example/"))
	tampered := "A" + token[1:]
	if _, err := issuer.ValidateClick(tampered, "https://a.example/", 1); err == nil {
		t.Fatal("tampered token validated")
	}
	clock.Set(clock.Now().Add(impressionTokenLifetime))
	if _, err := issuer.ValidateClick(token, "https://a.example/", 1); err == nil {
		t.Fatal("expired token validated")
	}
	clock.Set(clock.Now().Add(-2 * impressionTokenLifetime))
	if _, err := issuer.ValidateClick(token, "https://a.example/", 1); err == nil {
		t.Fatal("future token validated")
	}
}

func TestIssuerRejectsReplayMembershipRankAndClickCap(t *testing.T) {
	issuer, _ := issuerFixture(t)
	results := displayedFixture(
		"https://a.example/",
		"https://b.example/",
		"https://c.example/",
		"https://d.example/",
		"https://e.example/",
	)
	token := mustIssueToken(t, issuer, results)
	if _, err := issuer.ValidateClick(token, "https://missing.example/", 1); err == nil {
		t.Fatal("nonmember click validated")
	}
	if _, err := issuer.ValidateClick(token, results[0].URLIdentity, 2); err == nil {
		t.Fatal("rank-mismatched click validated")
	}
	for _, position := range []int{0, MaximumImpressionPosition + 1} {
		if _, err := issuer.ValidateClick(token, results[0].URLIdentity, position); err == nil {
			t.Fatalf("position %d validated", position)
		}
	}
	for index := range maximumClicksPerImpression {
		if _, err := issuer.ValidateClick(token, results[index].URLIdentity, index+1); err != nil {
			t.Fatalf("click %d: %v", index, err)
		}
	}
	if _, err := issuer.ValidateClick(token, results[0].URLIdentity, 1); err == nil {
		t.Fatal("replayed click validated")
	}
	if _, err := issuer.ValidateClick(
		token,
		results[maximumClicksPerImpression].URLIdentity,
		maximumClicksPerImpression+1,
	); err == nil {
		t.Fatal("click above impression cap validated")
	}
}

func TestIssuerRejectsMalformedTokens(t *testing.T) {
	issuer, _ := issuerFixture(t)
	valid := mustIssueToken(t, issuer, displayedFixture("https://a.example/"))
	payloadPart, signaturePart, _ := strings.Cut(valid, ".")
	payload, decodeErr := base64.RawURLEncoding.DecodeString(payloadPart)
	if decodeErr != nil {
		t.Fatalf("decode fixture: %v", decodeErr)
	}
	malformed := []string{
		"",
		strings.Repeat("a", maximumImpressionTokenBytes+1),
		"no-separator",
		"a.b.c",
		"%.%",
		payloadPart + "." + signaturePart[:len(signaturePart)-1] + "A",
		signedPayload(issuer, []byte("{")),
		signedPayload(issuer, bytes.Replace(payload, []byte(`"v":1`), []byte(`"v":2`), 1)),
		signedPayload(issuer, bytes.Repeat([]byte("x"), maximumImpressionPayloadBytes+1)),
		signedPayload(issuer, marshalImpressionClaims(impressionClaims{
			query:           "query",
			issuedAt:        1_800_000_000,
			expiresAt:       1_800_000_100,
			nonce:           "bad",
			modelAssignment: "model",
			results:         displayedFixture("https://a.example/"),
		})),
	}
	for index, token := range malformed {
		if _, err := issuer.ValidateClick(token, "https://a.example/", 1); err == nil {
			t.Fatalf("malformed token %d validated", index)
		}
	}
}

type issueTestCase struct {
	query   string
	model   string
	results []DisplayedCandidate
}

func TestIssuerRejectsInvalidIssueEnvelope(t *testing.T) {
	issuer, _ := issuerFixture(t)
	valid := displayedFixture("https://a.example/", "https://b.example/")
	duplicatePosition := append([]DisplayedCandidate(nil), valid...)
	duplicatePosition[1].Position = duplicatePosition[0].Position
	duplicateURL := append([]DisplayedCandidate(nil), valid...)
	duplicateURL[1].URLIdentity = duplicateURL[0].URLIdentity
	tests := []issueTestCase{
		{query: " ", model: "model", results: valid},
		{query: strings.Repeat("q", maximumNormalizedQueryBytes+1), model: "model", results: valid},
		{query: "q", model: " ", results: valid},
		{query: "q", model: strings.Repeat("m", maximumModelAssignmentBytes+1), results: valid},
		{query: "q", model: "model"},
		{
			query:   "q",
			model:   "model",
			results: make([]DisplayedCandidate, MaximumImpressionResults+1),
		},
		{query: "q", model: "model", results: duplicatePosition},
		{query: "q", model: "model", results: duplicateURL},
	}
	for index, test := range tests {
		if _, err := issuer.Issue(test.query, test.model, test.results); err == nil {
			t.Fatalf("invalid issue envelope %d succeeded", index)
		}
	}
}

func TestIssuerRejectsInvalidDisplayedCandidates(t *testing.T) {
	issuer, _ := issuerFixture(t)
	invalidCandidates := []DisplayedCandidate{
		candidateFixture(
			Candidate{ClusterIdentity: "cluster", Position: 1},
			0.5,
			AttributionOriginal,
			0,
		),
		candidateFixture(
			Candidate{
				URLIdentity:     strings.Repeat("u", maximumURLIdentityBytes+1),
				ClusterIdentity: "cluster",
				Position:        1,
			},
			0.5,
			AttributionOriginal,
			0,
		),
		candidateFixture(Candidate{URLIdentity: "url", Position: 1}, 0.5, AttributionOriginal, 0),
		candidateFixture(
			Candidate{
				URLIdentity:     "url",
				ClusterIdentity: strings.Repeat("c", maximumClusterIdentityBytes+1),
				Position:        1,
			},
			0.5,
			AttributionOriginal,
			0,
		),
		candidateFixture(
			Candidate{URLIdentity: "url", ClusterIdentity: "cluster"},
			0.5,
			AttributionOriginal,
			0,
		),
		candidateFixture(
			Candidate{
				URLIdentity:     "url",
				ClusterIdentity: "cluster",
				Position:        MaximumImpressionPosition + 1,
			},
			0.5,
			AttributionOriginal,
			0,
		),
		candidateFixture(validCandidate(), math.NaN(), AttributionOriginal, 0),
		candidateFixture(validCandidate(), math.Inf(1), AttributionOriginal, 0),
		candidateFixture(validCandidate(), -0.1, AttributionOriginal, 0),
		candidateFixture(validCandidate(), 1.1, AttributionOriginal, 0),
		candidateFixture(validCandidate(), minimumMeasuredPropensity/2, AttributionOriginal, 0),
		candidateFixture(validCandidate(), 0.5, "", 0),
		candidateFixture(
			validCandidate(),
			0.5,
			strings.Repeat("a", maximumAttributionBytes+1),
			0,
		),
		candidateFixture(validCandidate(), 0.5, AttributionOriginal, -1),
		candidateFixture(validCandidate(), 0.5, AttributionOriginal, MaximumImpressionResults),
	}
	for index, candidate := range invalidCandidates {
		if _, err := issuer.Issue(
			"q",
			"model",
			[]DisplayedCandidate{candidate},
		); err == nil {
			t.Fatalf("invalid displayed candidate %d succeeded", index)
		}
	}
}

func TestIssuerRejectsOversizePayload(t *testing.T) {
	issuer, _ := issuerFixture(t)
	large := make([]DisplayedCandidate, MaximumImpressionResults)
	for index := range large {
		large[index] = candidateFixture(
			Candidate{
				URLIdentity: fmt.Sprintf(
					"https://example/%02d/%s",
					index,
					strings.Repeat("u", 900),
				),
				ClusterIdentity: fmt.Sprintf(
					"cluster-%02d-%s",
					index,
					strings.Repeat("c", 400),
				),
				Position: index + 1,
			},
			0.5,
			AttributionOriginal,
			index,
		)
	}
	if _, err := issuer.Issue("q", "model", large); err == nil {
		t.Fatal("oversize impression payload succeeded")
	}
}

func TestIssuerRejectsInvalidCompletedClaimsAndEncodedText(t *testing.T) {
	valid := impressionClaims{
		query:           "q",
		issuedAt:        100,
		expiresAt:       101,
		nonce:           base64.RawURLEncoding.EncodeToString(make([]byte, impressionNonceBytes)),
		modelAssignment: "model",
		results:         displayedFixture("https://a.example/"),
	}
	invalid := []impressionClaims{
		withClaimTimes(valid, 0, 1),
		withClaimTimes(valid, 100, 100),
		withClaimTimes(valid, 100, 100+int64(impressionTokenLifetime/time.Second)+1),
		withClaimNonce(valid, "%"),
		withClaimNonce(valid, base64.RawURLEncoding.EncodeToString([]byte("short"))),
	}
	for index, claims := range invalid {
		if err := validateClaims(claims, true); err == nil {
			t.Fatalf("invalid completed claims %d validated", index)
		}
	}
	for _, encoded := range []string{"%", "", base64.RawURLEncoding.EncodeToString([]byte("toolong"))} {
		if _, err := decodeTokenString(encoded, 3); err == nil {
			t.Fatalf("encoded text %q decoded", encoded)
		}
	}
}

func TestIssuerEntropyFailuresAndReplayEviction(t *testing.T) {
	if _, err := NewIssuer(&failingEntropy{}, time.Now); err == nil {
		t.Fatal("issuer accepted missing key entropy")
	}
	issuer, err := NewIssuer(&failingEntropy{remaining: impressionKeyBytes}, time.Now)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	if _, err := issuer.experimentSeed(); err == nil {
		t.Fatal("experiment seed succeeded without entropy")
	}
	if _, err := issuer.Issue("q", "model", displayedFixture("https://a.example/")); err == nil {
		t.Fatal("token issue succeeded without nonce entropy")
	}

	evictionIssuer, _ := issuerFixture(t)
	for index := range maximumReplayTokens + 1 {
		result := displayedFixture("https://example/" + string(rune(index+1)))
		token := mustIssueToken(t, evictionIssuer, result)
		if _, err := evictionIssuer.ValidateClick(token, result[0].URLIdentity, 1); err != nil {
			t.Fatalf("record replay fixture %d: %v", index, err)
		}
	}
	if len(evictionIssuer.replays) != maximumReplayTokens {
		t.Fatalf("replay cache length = %d", len(evictionIssuer.replays))
	}
}

func TestIssuerReplayCleanup(t *testing.T) {
	issuer, clock := issuerFixture(t)
	issuer.replays["expired"] = replayState{
		expiresAt: clock.Now().Add(-time.Second),
		clicked:   map[string]struct{}{"old": {}},
	}
	result := displayedFixture("https://a.example/")
	token := mustIssueToken(t, issuer, result)
	if _, err := issuer.ValidateClick(token, result[0].URLIdentity, 1); err != nil {
		t.Fatalf("ValidateClick: %v", err)
	}
	if _, present := issuer.replays["expired"]; present {
		t.Fatal("expired replay entry was retained")
	}
}

func TestUnmarshalImpressionClaimsRejectsEncodedFields(t *testing.T) {
	document := impressionDocument{
		Version:         impressionTokenVersion,
		Query:           encodeTokenString("query"),
		IssuedAt:        100,
		ExpiresAt:       200,
		Nonce:           base64.RawURLEncoding.EncodeToString(make([]byte, impressionNonceBytes)),
		ModelAssignment: encodeTokenString("model"),
		Results: []impressionResultDocument{{
			URLIdentity:     encodeTokenString("url"),
			ClusterIdentity: encodeTokenString("cluster"),
			Position:        1,
			Propensity:      0.5,
			Attribution:     encodeTokenString(AttributionOriginal),
		}},
	}
	mutations := []func(*impressionDocument){
		func(candidate *impressionDocument) { candidate.Query = "%" },
		func(candidate *impressionDocument) { candidate.ModelAssignment = "%" },
		func(candidate *impressionDocument) { candidate.Results[0].URLIdentity = "%" },
		func(candidate *impressionDocument) { candidate.Results[0].ClusterIdentity = "%" },
		func(candidate *impressionDocument) { candidate.Results[0].Attribution = "%" },
	}
	for index, mutate := range mutations {
		candidate := document
		candidate.Results = append([]impressionResultDocument(nil), document.Results...)
		mutate(&candidate)
		encoded, err := json.Marshal(candidate)
		if err != nil {
			t.Fatalf("Marshal mutation %d: %v", index, err)
		}
		if _, err := unmarshalImpressionClaims(encoded); err == nil {
			t.Fatalf("encoded-field mutation %d decoded", index)
		}
	}
}

func TestIssuerConcurrentReplayCap(t *testing.T) {
	issuer, _ := issuerFixture(t)
	results := displayedFixture(
		"https://a.example/",
		"https://b.example/",
		"https://c.example/",
		"https://d.example/",
		"https://e.example/",
		"https://f.example/",
	)
	token := mustIssueToken(t, issuer, results)
	var successes atomic.Int64
	var wait sync.WaitGroup
	for index := range len(results) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := issuer.ValidateClick(
				token,
				results[index].URLIdentity,
				index+1,
			); err == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != maximumClicksPerImpression {
		t.Fatalf("concurrent successes = %d", successes.Load())
	}
}

func TestNewIssuerDefaults(t *testing.T) {
	issuer, err := NewIssuer(nil, nil)
	if err != nil {
		t.Fatalf("NewIssuer defaults: %v", err)
	}
	if _, err := issuer.Issue("q", "model", displayedFixture("https://a.example/")); err != nil {
		t.Fatalf("Issue defaults: %v", err)
	}
}

func issuerFixture(t *testing.T) (*Issuer, *mutableClock) {
	t.Helper()
	clock := newMutableClock(time.Unix(1_800_000_000, 0))
	issuer, err := NewIssuer(&sequenceEntropy{}, clock.Now)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	return issuer, clock
}

func displayedFixture(urls ...string) []DisplayedCandidate {
	results := make([]DisplayedCandidate, len(urls))
	for index, url := range urls {
		results[index] = candidateFixture(
			Candidate{
				URLIdentity:     url,
				ClusterIdentity: "cluster-" + string(rune('0'+index)),
				Position:        index + 1,
			},
			0.5,
			AttributionOriginal,
			index,
		)
	}

	return results
}

func candidateFixture(
	candidate Candidate,
	propensity float64,
	attribution string,
	originalIndex int,
) DisplayedCandidate {
	return DisplayedCandidate{
		Candidate:     candidate,
		OriginalIndex: originalIndex,
		Propensity:    propensity,
		Attribution:   attribution,
	}
}

func validCandidate() Candidate {
	return Candidate{URLIdentity: "url", ClusterIdentity: "cluster", Position: 1}
}

func mustIssueToken(t *testing.T, issuer *Issuer, results []DisplayedCandidate) string {
	t.Helper()
	token, err := issuer.Issue("query", "model", results)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	return token
}

func signedPayload(issuer *Issuer, payload []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(issuer.sign(payload))
}

func withClaimTimes(claims impressionClaims, issuedAt, expiresAt int64) impressionClaims {
	claims.issuedAt = issuedAt
	claims.expiresAt = expiresAt

	return claims
}

func withClaimNonce(claims impressionClaims, nonce string) impressionClaims {
	claims.nonce = nonce

	return claims
}
