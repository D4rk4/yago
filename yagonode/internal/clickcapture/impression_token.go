package clickcapture

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MaximumImpressionResults      = 64
	MaximumImpressionPosition     = 1000
	minimumMeasuredPropensity     = 0.05
	impressionTokenLifetime       = 10 * time.Minute
	maximumImpressionTokenBytes   = 65536
	maximumImpressionPayloadBytes = 48000
	maximumNormalizedQueryBytes   = 512
	maximumURLIdentityBytes       = 2048
	maximumClusterIdentityBytes   = 512
	maximumModelAssignmentBytes   = 64
	maximumAttributionBytes       = 64
	impressionNonceBytes          = 16
	impressionKeyBytes            = 32
	maximumReplayTokens           = 4096
	maximumClicksPerImpression    = 4
	impressionTokenVersion        = 1
)

type Issuer struct {
	key     []byte
	entropy io.Reader
	clock   func() time.Time
	mutex   sync.Mutex
	replays map[string]replayState
}

type replayState struct {
	expiresAt time.Time
	clicked   map[string]struct{}
}

type impressionClaims struct {
	query           string
	issuedAt        int64
	expiresAt       int64
	nonce           string
	modelAssignment string
	results         []DisplayedCandidate
}

type impressionDocument struct {
	Version         int                        `json:"v"`
	Query           string                     `json:"q"`
	IssuedAt        int64                      `json:"iat"`
	ExpiresAt       int64                      `json:"exp"`
	Nonce           string                     `json:"n"`
	ModelAssignment string                     `json:"m"`
	Results         []impressionResultDocument `json:"r"`
}

type impressionResultDocument struct {
	URLIdentity     string  `json:"u"`
	ClusterIdentity string  `json:"c"`
	Position        int     `json:"p"`
	Propensity      float64 `json:"e"`
	Attribution     string  `json:"a"`
	OriginalIndex   int     `json:"o"`
}

type ValidatedClick struct {
	Query           string
	ModelAssignment string
	Candidate       DisplayedCandidate
	Pair            *FairPairMember
}

func NewIssuer(entropy io.Reader, clock func() time.Time) (*Issuer, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	if clock == nil {
		clock = time.Now
	}
	key := make([]byte, impressionKeyBytes)
	if _, err := io.ReadFull(entropy, key); err != nil {
		return nil, fmt.Errorf("read impression signing key: %w", err)
	}

	return &Issuer{
		key:     key,
		entropy: entropy,
		clock:   clock,
		replays: map[string]replayState{},
	}, nil
}

func (i *Issuer) Issue(
	query string,
	modelAssignment string,
	results []DisplayedCandidate,
) (string, error) {
	token, _, err := i.issue(query, modelAssignment, results)

	return token, err
}

func (i *Issuer) issue(
	query string,
	modelAssignment string,
	results []DisplayedCandidate,
) (string, impressionClaims, error) {
	claims := impressionClaims{
		query:           normalizeQuery(query),
		modelAssignment: strings.TrimSpace(modelAssignment),
		results:         append([]DisplayedCandidate(nil), results...),
	}
	if err := validateClaims(claims, false); err != nil {
		return "", impressionClaims{}, err
	}
	nonce := make([]byte, impressionNonceBytes)
	i.mutex.Lock()
	_, entropyErr := io.ReadFull(i.entropy, nonce)
	i.mutex.Unlock()
	if entropyErr != nil {
		return "", impressionClaims{}, fmt.Errorf("read impression nonce: %w", entropyErr)
	}
	now := i.clock().UTC()
	claims.issuedAt = now.Unix()
	claims.expiresAt = now.Add(impressionTokenLifetime).Unix()
	claims.nonce = base64.RawURLEncoding.EncodeToString(nonce)
	payload := marshalImpressionClaims(claims)
	if len(payload) > maximumImpressionPayloadBytes {
		return "", impressionClaims{}, fmt.Errorf(
			"impression payload exceeds %d bytes",
			maximumImpressionPayloadBytes,
		)
	}
	signature := i.sign(payload)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature)

	return token, claims, nil
}

func (i *Issuer) experimentSeed() (uint64, error) {
	seedBytes := make([]byte, 8)
	i.mutex.Lock()
	_, err := io.ReadFull(i.entropy, seedBytes)
	i.mutex.Unlock()
	if err != nil {
		return 0, fmt.Errorf("read impression experiment seed: %w", err)
	}

	return binary.LittleEndian.Uint64(seedBytes), nil
}

func (i *Issuer) ValidateClick(
	token string,
	urlIdentity string,
	position int,
) (ValidatedClick, error) {
	claims, err := i.parse(token)
	if err != nil {
		return ValidatedClick{}, err
	}
	if position < 1 || position > MaximumImpressionPosition {
		return ValidatedClick{}, fmt.Errorf("click position is outside the impression bound")
	}
	var matched *DisplayedCandidate
	for index := range claims.results {
		candidate := &claims.results[index]
		if candidate.Position == position && candidate.URLIdentity == urlIdentity {
			matched = candidate
			break
		}
	}
	if matched == nil {
		return ValidatedClick{}, fmt.Errorf("click does not match the signed impression")
	}
	if err := i.consume(token, matched.URLIdentity, time.Unix(claims.expiresAt, 0)); err != nil {
		return ValidatedClick{}, err
	}

	return ValidatedClick{
		Query:           claims.query,
		ModelAssignment: claims.modelAssignment,
		Candidate:       *matched,
		Pair:            pairedFairPairMember(claims.results, *matched),
	}, nil
}

func (i *Issuer) parse(token string) (impressionClaims, error) {
	if len(token) == 0 || len(token) > maximumImpressionTokenBytes {
		return impressionClaims{}, fmt.Errorf("impression token length is invalid")
	}
	payloadPart, signaturePart, separated := strings.Cut(token, ".")
	if !separated || strings.Contains(signaturePart, ".") {
		return impressionClaims{}, fmt.Errorf("impression token shape is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil || len(payload) > maximumImpressionPayloadBytes {
		return impressionClaims{}, fmt.Errorf("impression token payload is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil || !hmac.Equal(signature, i.sign(payload)) {
		return impressionClaims{}, fmt.Errorf("impression token signature is invalid")
	}
	claims, err := unmarshalImpressionClaims(payload)
	if err != nil {
		return impressionClaims{}, err
	}
	if err := validateClaims(claims, true); err != nil {
		return impressionClaims{}, err
	}
	now := i.clock().UTC().Unix()
	if now < claims.issuedAt || now >= claims.expiresAt {
		return impressionClaims{}, fmt.Errorf("impression token is expired")
	}

	return claims, nil
}

func (i *Issuer) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, i.key)
	_, _ = mac.Write(payload)

	return mac.Sum(nil)
}

func (i *Issuer) consume(token, identity string, expiresAt time.Time) error {
	keyBytes := sha256.Sum256([]byte(token))
	key := base64.RawURLEncoding.EncodeToString(keyBytes[:])
	now := i.clock().UTC()
	i.mutex.Lock()
	defer i.mutex.Unlock()
	for replayKey, state := range i.replays {
		if !state.expiresAt.After(now) {
			delete(i.replays, replayKey)
		}
	}
	state, exists := i.replays[key]
	if !exists && len(i.replays) >= maximumReplayTokens {
		i.evictReplay()
	}
	if state.clicked == nil {
		state = replayState{expiresAt: expiresAt, clicked: map[string]struct{}{}}
	}
	if _, duplicate := state.clicked[identity]; duplicate {
		return fmt.Errorf("impression click was already recorded")
	}
	if len(state.clicked) >= maximumClicksPerImpression {
		return fmt.Errorf("impression click cap reached")
	}
	state.clicked[identity] = struct{}{}
	i.replays[key] = state

	return nil
}

func (i *Issuer) evictReplay() {
	oldestKey := ""
	var oldestExpiry time.Time
	for key, state := range i.replays {
		if oldestKey == "" || state.expiresAt.Before(oldestExpiry) ||
			state.expiresAt.Equal(oldestExpiry) && key < oldestKey {
			oldestKey = key
			oldestExpiry = state.expiresAt
		}
	}
	delete(i.replays, oldestKey)
}

func validateClaims(claims impressionClaims, complete bool) error {
	if claims.query == "" || len(claims.query) > maximumNormalizedQueryBytes ||
		claims.query != normalizeQuery(claims.query) {
		return fmt.Errorf("impression query is invalid")
	}
	if claims.modelAssignment == "" ||
		len(claims.modelAssignment) > maximumModelAssignmentBytes {
		return fmt.Errorf("impression model assignment is invalid")
	}
	if len(claims.results) == 0 || len(claims.results) > MaximumImpressionResults {
		return fmt.Errorf("impression result count is invalid")
	}
	positions := make(map[int]struct{}, len(claims.results))
	urls := make(map[string]struct{}, len(claims.results))
	for _, result := range claims.results {
		if err := validateDisplayedCandidate(result); err != nil {
			return err
		}
		if _, exists := positions[result.Position]; exists {
			return fmt.Errorf("impression positions must be unique")
		}
		if _, exists := urls[result.URLIdentity]; exists {
			return fmt.Errorf("impression URL identities must be unique")
		}
		positions[result.Position] = struct{}{}
		urls[result.URLIdentity] = struct{}{}
	}
	if !complete {
		return nil
	}
	if claims.issuedAt <= 0 || claims.expiresAt <= claims.issuedAt ||
		claims.expiresAt-claims.issuedAt > int64(impressionTokenLifetime/time.Second) {
		return fmt.Errorf("impression token lifetime is invalid")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(claims.nonce)
	if err != nil || len(nonce) != impressionNonceBytes {
		return fmt.Errorf("impression nonce is invalid")
	}

	return nil
}

func validateDisplayedCandidate(result DisplayedCandidate) error {
	if result.URLIdentity == "" || len(result.URLIdentity) > maximumURLIdentityBytes {
		return fmt.Errorf("impression URL identity is invalid")
	}
	if result.ClusterIdentity == "" ||
		len(result.ClusterIdentity) > maximumClusterIdentityBytes {
		return fmt.Errorf("impression cluster identity is invalid")
	}
	if result.Position < 1 || result.Position > MaximumImpressionPosition {
		return fmt.Errorf("impression position is invalid")
	}
	if math.IsNaN(result.Propensity) || math.IsInf(result.Propensity, 0) ||
		result.Propensity < 0 || result.Propensity > 1 ||
		result.Propensity > 0 && result.Propensity < minimumMeasuredPropensity {
		return fmt.Errorf("impression propensity is invalid")
	}
	if result.Attribution == "" || len(result.Attribution) > maximumAttributionBytes {
		return fmt.Errorf("impression attribution is invalid")
	}
	if result.OriginalIndex < 0 || result.OriginalIndex >= MaximumImpressionResults {
		return fmt.Errorf("impression source index is invalid")
	}

	return nil
}

func marshalImpressionClaims(claims impressionClaims) []byte {
	encoded := []byte(`{"v":1,"q":"`)
	encoded = append(encoded, encodeTokenString(claims.query)...)
	encoded = append(encoded, `","iat":`...)
	encoded = strconv.AppendInt(encoded, claims.issuedAt, 10)
	encoded = append(encoded, `,"exp":`...)
	encoded = strconv.AppendInt(encoded, claims.expiresAt, 10)
	encoded = append(encoded, `,"n":"`...)
	encoded = append(encoded, claims.nonce...)
	encoded = append(encoded, `","m":"`...)
	encoded = append(encoded, encodeTokenString(claims.modelAssignment)...)
	encoded = append(encoded, `","r":[`...)
	for index, result := range claims.results {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, `{"u":"`...)
		encoded = append(encoded, encodeTokenString(result.URLIdentity)...)
		encoded = append(encoded, `","c":"`...)
		encoded = append(encoded, encodeTokenString(result.ClusterIdentity)...)
		encoded = append(encoded, `","p":`...)
		encoded = strconv.AppendInt(encoded, int64(result.Position), 10)
		encoded = append(encoded, `,"e":`...)
		encoded = strconv.AppendFloat(encoded, result.Propensity, 'g', -1, 64)
		encoded = append(encoded, `,"a":"`...)
		encoded = append(encoded, encodeTokenString(result.Attribution)...)
		encoded = append(encoded, `","o":`...)
		encoded = strconv.AppendInt(encoded, int64(result.OriginalIndex), 10)
		encoded = append(encoded, '}')
	}
	encoded = append(encoded, ']', '}')

	return encoded
}

func unmarshalImpressionClaims(payload []byte) (impressionClaims, error) {
	var document impressionDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return impressionClaims{}, fmt.Errorf("decode impression token: %w", err)
	}
	if document.Version != impressionTokenVersion {
		return impressionClaims{}, fmt.Errorf("impression token version is unsupported")
	}
	query, err := decodeTokenString(document.Query, maximumNormalizedQueryBytes)
	if err != nil {
		return impressionClaims{}, err
	}
	model, err := decodeTokenString(document.ModelAssignment, maximumModelAssignmentBytes)
	if err != nil {
		return impressionClaims{}, err
	}
	results := make([]DisplayedCandidate, len(document.Results))
	for index, result := range document.Results {
		urlIdentity, decodeErr := decodeTokenString(result.URLIdentity, maximumURLIdentityBytes)
		if decodeErr != nil {
			return impressionClaims{}, decodeErr
		}
		clusterIdentity, decodeErr := decodeTokenString(
			result.ClusterIdentity,
			maximumClusterIdentityBytes,
		)
		if decodeErr != nil {
			return impressionClaims{}, decodeErr
		}
		attribution, decodeErr := decodeTokenString(result.Attribution, maximumAttributionBytes)
		if decodeErr != nil {
			return impressionClaims{}, decodeErr
		}
		results[index] = DisplayedCandidate{
			Candidate: Candidate{
				URLIdentity:     urlIdentity,
				ClusterIdentity: clusterIdentity,
				Position:        result.Position,
			},
			OriginalIndex: result.OriginalIndex,
			Propensity:    result.Propensity,
			Attribution:   attribution,
		}
	}

	return impressionClaims{
		query:           query,
		issuedAt:        document.IssuedAt,
		expiresAt:       document.ExpiresAt,
		nonce:           document.Nonce,
		modelAssignment: model,
		results:         results,
	}, nil
}

func encodeTokenString(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeTokenString(encoded string, maximumBytes int) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > maximumBytes {
		return "", fmt.Errorf("impression token text is invalid")
	}

	return string(decoded), nil
}

func normalizeQuery(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(raw)), " ")
}
