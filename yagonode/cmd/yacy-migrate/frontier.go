package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	maximumFrontierSeenURLs = 1_000_000
)

type frontierImportOptions struct {
	Input        string
	Profiles     string
	Endpoint     string
	APIKey       string
	State        string
	BatchRecords int
	MaxRecords   int
	DryRun       bool
}

type frontierFileHeader struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	SourceID string `json:"sourceId"`
}

type yaCyFrontierRecord struct {
	Type           string `json:"type"`
	Version        int    `json:"version"`
	Stack          string `json:"stack"`
	URL            string `json:"url"`
	ProfileHandle  string `json:"profileHandle"`
	Depth          int    `json:"depth"`
	Anchor         string `json:"anchor"`
	InitiatorHex   string `json:"initiatorHex"`
	QueuedAtMillis int64  `json:"queuedAtMillis"`
}

type yaCyProfileRecord struct {
	Type    string            `json:"type"`
	Version int               `json:"version"`
	State   string            `json:"state"`
	Handle  string            `json:"handle"`
	Fields  map[string]string `json:"fields"`
}

type frontierImportCheckpoint struct {
	Version                int    `json:"version"`
	SourceID               string `json:"sourceId"`
	InputSHA256            string `json:"inputSha256"`
	ProfilesSHA256         string `json:"profilesSha256"`
	DestinationFingerprint string `json:"destinationFingerprint,omitempty"`
	InputSize              int64  `json:"inputSize"`
	InputModTime           int64  `json:"inputModTime"`
	ProfilesSize           int64  `json:"profilesSize"`
	ProfilesModTime        int64  `json:"profilesModTime"`
	Offset                 int64  `json:"offset"`
	Records                int64  `json:"records"`
	UniqueRequests         int64  `json:"uniqueRequests"`
	EnqueuedRequests       int64  `json:"enqueuedRequests"`
	AlreadyIndexed         int64  `json:"alreadyIndexed"`
	DuplicateRequests      int64  `json:"duplicateRequests"`
	BlockedCredentialURLs  int64  `json:"blockedCredentialURLs"`
	BlockedInvalidURLs     int64  `json:"blockedInvalidURLs"`
	Complete               bool   `json:"complete,omitempty"`
}

type frontierImportResponse struct {
	EnqueuedRequests int  `json:"enqueuedRequests"`
	AlreadyIndexed   int  `json:"alreadyIndexed"`
	DuplicateBatch   bool `json:"duplicateBatch,omitempty"`
}

type frontierSourceProfile struct {
	sourceHandle string
	profile      yagocrawlcontract.CrawlProfile
}

type frontierSourceData struct {
	inputInfo       os.FileInfo
	profileInfo     os.FileInfo
	inputHash       string
	profilesHash    string
	profileSourceID string
	profiles        map[string]frontierSourceProfile
}

type frontierImportSession struct {
	input     *os.File
	inputInfo os.FileInfo
	reader    *bufio.Reader
	profiles  map[string]frontierSourceProfile
	seen      map[string]struct{}
	endpoint  *url.URL
	statePath string
	state     frontierImportCheckpoint
}

func importFrontier(
	ctx context.Context,
	opts frontierImportOptions,
	progress io.Writer,
) (resultErr error) {
	if err := validateFrontierImportOptions(opts); err != nil {
		return err
	}
	input, err := os.Open(opts.Input) // #nosec G304 -- selected by local migration operator.
	if err != nil {
		return fmt.Errorf("open YaCy frontier: %w", err)
	}
	defer func() {
		resultErr = closeImportInput(input, "close YaCy frontier", resultErr)
	}()
	profileFile, err := os.Open(
		opts.Profiles,
	) // #nosec G304 -- selected by local migration operator.
	if err != nil {
		return fmt.Errorf("open YaCy profile sidecar: %w", err)
	}
	defer func() {
		resultErr = closeImportInput(profileFile, "close YaCy profile sidecar", resultErr)
	}()
	source, err := loadFrontierSourceData(input, profileFile)
	if err != nil {
		return err
	}
	session, err := prepareFrontierSession(input, opts, source)
	if err != nil {
		return err
	}
	return runFrontierImport(ctx, opts, progress, session)
}

func loadFrontierSourceData(input, profileFile *os.File) (frontierSourceData, error) {
	inputInfo, err := input.Stat()
	if err != nil {
		return frontierSourceData{}, fmt.Errorf("stat YaCy frontier: %w", err)
	}
	profileInfo, err := profileFile.Stat()
	if err != nil {
		return frontierSourceData{}, fmt.Errorf("stat YaCy profile sidecar: %w", err)
	}
	profilesHash, err := hashOpenFile(profileFile)
	if err != nil {
		return frontierSourceData{}, fmt.Errorf("fingerprint YaCy profile sidecar: %w", err)
	}
	profiles, profileSourceID, err := loadYaCyProfiles(profileFile)
	if err != nil {
		return frontierSourceData{}, err
	}
	inputHash, err := hashOpenFile(input)
	if err != nil {
		return frontierSourceData{}, fmt.Errorf("fingerprint YaCy frontier: %w", err)
	}
	return frontierSourceData{
		inputInfo: inputInfo, profileInfo: profileInfo, inputHash: inputHash,
		profilesHash: profilesHash, profileSourceID: profileSourceID, profiles: profiles,
	}, nil
}

func prepareFrontierSession(
	input *os.File,
	opts frontierImportOptions,
	source frontierSourceData,
) (*frontierImportSession, error) {
	endpoint, err := validateFrontierEndpoint(opts.Endpoint, opts.DryRun)
	if err != nil {
		return nil, err
	}
	statePath := opts.State
	if statePath == "" {
		statePath = filepath.Clean(opts.Input) + ".frontier.import.state.json"
	}
	state, found, err := loadValidatedFrontierCheckpoint(opts, source, endpoint, statePath)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(input, 64<<10)
	header, headerEnd, err := readFrontierHeader(reader)
	if err != nil {
		return nil, err
	}
	if header.SourceID != source.profileSourceID {
		return nil, errors.New("YaCy frontier and profile sidecar source IDs do not match")
	}
	if opts.DryRun || !found {
		state = newFrontierCheckpoint(source, header.SourceID, headerEnd)
		if !opts.DryRun {
			state.DestinationFingerprint = destinationFingerprint(endpoint)
			if err := saveCheckpoint(statePath, state); err != nil {
				return nil, err
			}
		}
	}
	seen := make(map[string]struct{})
	seekOffset := headerEnd
	if state.Offset > headerEnd {
		seen, err = rebuildFrontierSeen(input, state.Offset, source.profiles)
		if err != nil {
			return nil, fmt.Errorf("restore frontier deduplication state: %w", err)
		}
		seekOffset = state.Offset
	}
	if err := seekFrontierImportInput(input, seekOffset); err != nil {
		return nil, fmt.Errorf("seek YaCy frontier checkpoint: %w", err)
	}
	reader.Reset(input)
	return &frontierImportSession{
		input: input, inputInfo: source.inputInfo, reader: reader,
		profiles: source.profiles, seen: seen, endpoint: endpoint,
		statePath: statePath, state: state,
	}, nil
}

func seekFrontierImportInput(input io.Seeker, offset int64) error {
	_, err := input.Seek(offset, io.SeekStart)
	if err != nil {
		return fmt.Errorf("seek frontier input: %w", err)
	}
	return nil
}

func loadValidatedFrontierCheckpoint(
	opts frontierImportOptions,
	source frontierSourceData,
	endpoint *url.URL,
	statePath string,
) (frontierImportCheckpoint, bool, error) {
	state, found, err := loadFrontierImportCheckpoint(statePath)
	if err != nil {
		return frontierImportCheckpoint{}, false, err
	}
	if err := validateFrontierCheckpoint(frontierCheckpointValidation{
		state: state, found: found, inputInfo: source.inputInfo, profileInfo: source.profileInfo,
		inputHash: source.inputHash, profilesHash: source.profilesHash,
		sourceID: source.profileSourceID, endpoint: endpoint, dryRun: opts.DryRun,
	}); err != nil {
		return frontierImportCheckpoint{}, false, err
	}
	return state, found, nil
}

func newFrontierCheckpoint(
	source frontierSourceData,
	sourceID string,
	headerEnd int64,
) frontierImportCheckpoint {
	return frontierImportCheckpoint{
		Version: 1, SourceID: sourceID, InputSHA256: source.inputHash,
		ProfilesSHA256: source.profilesHash, InputSize: source.inputInfo.Size(),
		InputModTime:    source.inputInfo.ModTime().UnixNano(),
		ProfilesSize:    source.profileInfo.Size(),
		ProfilesModTime: source.profileInfo.ModTime().UnixNano(), Offset: headerEnd,
	}
}

func validateFrontierImportOptions(opts frontierImportOptions) error {
	if strings.TrimSpace(opts.Input) == "" || strings.TrimSpace(opts.Profiles) == "" {
		return errors.New("-input and -profiles are required")
	}
	if opts.BatchRecords < 1 ||
		opts.BatchRecords > yagocrawlcontract.MaximumCrawlOrderImportBatchRecords {
		return fmt.Errorf(
			"-batch-records must be between 1 and %d",
			yagocrawlcontract.MaximumCrawlOrderImportBatchRecords,
		)
	}
	if opts.MaxRecords < 0 {
		return errors.New("-max-records must not be negative")
	}
	if !opts.DryRun && opts.APIKey == "" {
		return errors.New("YAGO_API_KEY is required for a live frontier import")
	}
	return nil
}

func validateFrontierEndpoint(raw string, dryRun bool) (*url.URL, error) {
	if raw == "" && dryRun {
		return &url.URL{
			Scheme: "http",
			Host:   "dry-run.invalid",
			Path:   "/admin/crawl/frontier/import",
		}, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Path != "/admin/crawl/frontier/import" {
		return nil, errors.New(
			"-endpoint must be an HTTP(S) /admin/crawl/frontier/import URL without credentials or query",
		)
	}
	return parsed, nil
}

func readFrontierHeader(reader *bufio.Reader) (frontierFileHeader, int64, error) {
	line, err := readBoundedLine(reader)
	if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
		return frontierFileHeader{}, 0, fmt.Errorf("read YaCy frontier header: %w", err)
	}
	var header frontierFileHeader
	if json.Unmarshal(line, &header) != nil ||
		(header.Type != "yacy-crawl-frontier" && header.Type != "yacy-queue-frontier") ||
		header.Version != 1 || !validFrontierSourceID(header.SourceID) {
		return frontierFileHeader{}, 0, errors.New("invalid versioned YaCy frontier header")
	}
	return header, int64(len(line)), nil
}

func loadYaCyProfiles(file *os.File) (map[string]frontierSourceProfile, string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", fmt.Errorf("seek YaCy profile sidecar: %w", err)
	}
	reader := bufio.NewReaderSize(file, 16<<10)
	line, err := readBoundedLine(reader)
	if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
		return nil, "", fmt.Errorf("read YaCy profile sidecar header: %w", err)
	}
	var header frontierFileHeader
	if json.Unmarshal(line, &header) != nil || header.Type != "yacy-crawl-profiles" ||
		header.Version != 1 || !validFrontierSourceID(header.SourceID) {
		return nil, "", errors.New("invalid versioned YaCy profile sidecar header")
	}
	profiles := make(map[string]frontierSourceProfile)
	for {
		record, done, err := readYaCyProfileRecord(reader)
		if err != nil {
			return nil, "", err
		}
		if done {
			break
		}
		if _, duplicate := profiles[record.Handle]; duplicate {
			return nil, "", errors.New("duplicate YaCy crawl-profile handle in sidecar")
		}
		profile, err := mapYaCyProfile(record)
		if err != nil {
			return nil, "", fmt.Errorf("map YaCy crawl profile: %w", err)
		}
		profiles[record.Handle] = frontierSourceProfile{
			sourceHandle: record.Handle,
			profile:      profile,
		}
	}
	if len(profiles) == 0 {
		return nil, "", errors.New("YaCy profile sidecar contains no profiles")
	}
	return profiles, header.SourceID, nil
}

func readYaCyProfileRecord(reader *bufio.Reader) (yaCyProfileRecord, bool, error) {
	line, err := readBoundedLine(reader)
	if errors.Is(err, io.EOF) && len(line) == 0 {
		return yaCyProfileRecord{}, true, nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return yaCyProfileRecord{}, false, fmt.Errorf("read YaCy profile sidecar record: %w", err)
	}
	var record yaCyProfileRecord
	if json.Unmarshal(line, &record) != nil || record.Type != "yacy-crawl-profile" ||
		record.Version != 1 || record.Handle == "" || len(record.Fields) == 0 {
		return yaCyProfileRecord{}, false, errors.New("invalid YaCy crawl-profile record")
	}
	return record, false, nil
}

func mapYaCyProfile(record yaCyProfileRecord) (yagocrawlcontract.CrawlProfile, error) {
	fields := record.Fields
	profile := yagocrawlcontract.CrawlProfile{
		Name:                 "Imported YaCy profile " + record.Handle,
		Scope:                yagocrawlcontract.ScopeWide,
		URLMustMatch:         mapYaCyPattern(fields["crawlerURLMustMatch"]),
		URLMustNotMatch:      fields["crawlerURLMustNotMatch"],
		IndexURLMustMatch:    mapYaCyPattern(fields["indexURLMustMatch"]),
		IndexURLMustNotMatch: fields["indexURLMustNotMatch"],
		MaxDepth:             yagocrawlcontract.MaxCrawlDepth,
		AllowQueryURLs:       true,
		MaxPagesPerHost:      yagocrawlcontract.UnlimitedPagesPerHost,
		Formats:              yagocrawlcontract.DefaultFormatToggles(),
	}
	maximumPagesPerRun := yagocrawlcontract.MaximumCrawlOrderImportBatchRecords
	profile.MaxPagesPerRun = &maximumPagesPerRun
	if err := applyYaCyProfileLimits(fields, &profile); err != nil {
		return yagocrawlcontract.CrawlProfile{}, err
	}
	metadata := make(map[string]string, len(fields)+1)
	metadata["yacy.source_profile_handle"] = record.Handle
	for key, value := range fields {
		if len(key) > 240 || len(value) > 32<<10 {
			return yagocrawlcontract.CrawlProfile{}, errors.New(
				"YaCy profile field exceeds migration bounds",
			)
		}
		metadata["yacy.profile."+key] = value
	}
	profile.SourceMetadata = yagocrawlcontract.NewCrawlSourceMetadata(metadata)
	profile = yagocrawlcontract.NewCrawlProfile(profile)
	if err := profile.Validate(); err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf(
			"mapped YaCy crawl profile is not supported: %w",
			err,
		)
	}
	return profile, nil
}

func applyYaCyProfileLimits(
	fields map[string]string,
	profile *yagocrawlcontract.CrawlProfile,
) error {
	depth, err := parseNonnegativeYaCyInt(fields["generalDepth"], "generalDepth")
	if err != nil {
		return err
	}
	if depth > 0 {
		profile.MaxDepth = min(depth, yagocrawlcontract.MaxCrawlDepth)
	}
	pagesPerHost, err := parseYaCyPagesPerHost(fields["domMaxPages"])
	if err != nil {
		return err
	}
	if pagesPerHost > 0 {
		profile.MaxPagesPerHost = pagesPerHost
	}
	if raw := fields["obeyHtmlRobotsNofollow"]; raw != "" {
		obey, err := strconv.ParseBool(raw)
		if err != nil {
			return errors.New("invalid YaCy robots-nofollow setting")
		}
		profile.FollowNoFollowLinks = !obey
	}
	return validateYaCyRecrawlCutoff(fields["recrawlIfOlder"])
}

func parseYaCyPagesPerHost(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid YaCy domMaxPages")
	}
	return value, nil
}

func parseNonnegativeYaCyInt(raw, name string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid YaCy %s", name)
	}
	return value, nil
}

func validateYaCyRecrawlCutoff(raw string) error {
	if raw == "" || raw == "eternity" {
		return nil
	}
	cutoff, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cutoff < 0 {
		return errors.New("invalid YaCy recrawl cutoff")
	}
	return nil
}

func mapYaCyPattern(pattern string) string {
	if pattern == "" {
		return yagocrawlcontract.MatchAll
	}
	return pattern
}

func validFrontierSourceID(value string) bool {
	if value == "" || len(value) > yagocrawlcontract.MaximumCrawlOrderImportSourceID {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func runFrontierImport(
	ctx context.Context,
	opts frontierImportOptions,
	progress io.Writer,
	session *frontierImportSession,
) error {
	if session.state.Complete {
		return writeFrontierImportProgress(progress, session.state)
	}
	run := newFrontierBatchRun(ctx, opts, progress, session)
	return run.execute()
}

type frontierBatchRun struct {
	ctx            context.Context
	opts           frontierImportOptions
	progress       io.Writer
	session        *frontierImportSession
	client         *http.Client
	batch          []yagocrawlcontract.CrawlRequest
	batchEnd       int64
	profileHandle  string
	batchRows      int64
	duplicates     int64
	blockedCreds   int64
	blockedInvalid int64
	runRows        int
	offset         int64
	maximumSeen    int
}

func newFrontierBatchRun(
	ctx context.Context,
	opts frontierImportOptions,
	progress io.Writer,
	session *frontierImportSession,
) *frontierBatchRun {
	return &frontierBatchRun{
		ctx: ctx, opts: opts, progress: progress, session: session,
		client:   &http.Client{Timeout: 90 * time.Second},
		batchEnd: session.state.Offset, offset: session.state.Offset,
		maximumSeen: maximumFrontierSeenURLs,
	}
}

func (run *frontierBatchRun) execute() error {
	for run.opts.MaxRecords == 0 || run.runRows < run.opts.MaxRecords {
		row, lineEnd, done, err := run.readRow()
		if err != nil {
			return err
		}
		if done {
			break
		}
		if err := run.addRow(row, lineEnd); err != nil {
			return err
		}
	}
	if run.batchRows > 0 {
		if err := run.flush(); err != nil {
			return err
		}
	}
	if !run.opts.DryRun {
		run.session.state.Complete = run.opts.MaxRecords == 0 &&
			run.session.state.Offset == run.session.inputInfo.Size()
		if err := saveCheckpoint(run.session.statePath, run.session.state); err != nil {
			return err
		}
	}
	return writeFrontierImportSummary(run.progress, run.session.state, run.opts.DryRun)
}

func (run *frontierBatchRun) readRow() (yaCyFrontierRecord, int64, bool, error) {
	line, err := readBoundedLine(run.session.reader)
	if errors.Is(err, io.EOF) && len(line) == 0 {
		return yaCyFrontierRecord{}, run.offset, true, nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return yaCyFrontierRecord{}, run.offset, false,
			fmt.Errorf("read YaCy frontier at byte %d: %w", run.offset, err)
	}
	var row yaCyFrontierRecord
	if json.Unmarshal(line, &row) != nil || row.Type != "crawlRequest" || row.Version != 1 ||
		row.Stack != "LOCAL" || row.URL == "" || row.ProfileHandle == "" {
		return yaCyFrontierRecord{}, run.offset, false,
			fmt.Errorf("invalid YaCy frontier record at byte %d", run.offset)
	}
	if _, ok := run.session.profiles[row.ProfileHandle]; !ok {
		return yaCyFrontierRecord{}, run.offset, false,
			fmt.Errorf("YaCy frontier references an unavailable profile at byte %d", run.offset)
	}
	run.offset += int64(len(line))
	return row, run.offset, false, nil
}

func (run *frontierBatchRun) addRow(row yaCyFrontierRecord, lineEnd int64) error {
	blockedReason := frontierURLBlockReason(row.URL)
	if blockedReason == "" && len(run.batch) > 0 && run.profileHandle != row.ProfileHandle {
		if err := run.flush(); err != nil {
			return err
		}
	}
	run.runRows++
	run.batchRows++
	run.batchEnd = lineEnd
	if blockedReason != "" {
		return run.addBlockedRow(row, blockedReason)
	}
	return run.addRequestRow(row, lineEnd)
}

func (run *frontierBatchRun) addBlockedRow(row yaCyFrontierRecord, reason string) error {
	duplicate, err := run.recordSeen(row)
	if err != nil {
		return err
	}
	switch {
	case duplicate:
		run.duplicates++
	case reason == "credential":
		run.blockedCreds++
	default:
		run.blockedInvalid++
	}
	return run.flushFullBatch()
}

func (run *frontierBatchRun) addRequestRow(row yaCyFrontierRecord, lineEnd int64) error {
	profile := run.session.profiles[row.ProfileHandle].profile
	request, err := mapYaCyFrontierRequest(row, profile)
	if err != nil {
		return fmt.Errorf("map YaCy frontier record at byte %d: %w", lineEnd, err)
	}
	duplicate, err := run.recordSeen(row)
	if err != nil {
		return err
	}
	if duplicate {
		run.duplicates++
		return run.flushFullBatch()
	}
	run.batch = append(run.batch, request)
	if run.profileHandle == "" {
		run.profileHandle = row.ProfileHandle
	}
	return run.flushFullBatch()
}

func (run *frontierBatchRun) recordSeen(row yaCyFrontierRecord) (bool, error) {
	identity := row.ProfileHandle + "\x00" + row.URL
	maximum := run.maximumSeen
	if maximum == 0 {
		maximum = maximumFrontierSeenURLs
	}
	return addFrontierSeen(run.session.seen, identity, maximum)
}

func addFrontierSeen(seen map[string]struct{}, identity string, maximum int) (bool, error) {
	if _, duplicate := seen[identity]; duplicate {
		return true, nil
	}
	if len(seen) >= maximum {
		return false, errors.New("frontier deduplication safety limit reached")
	}
	seen[identity] = struct{}{}
	return false, nil
}

func (run *frontierBatchRun) flushFullBatch() error {
	if len(run.batch) < run.opts.BatchRecords {
		return nil
	}
	return run.flush()
}

func (run *frontierBatchRun) flush() error {
	if run.batchRows == 0 {
		return nil
	}
	importBatch, err := run.mappedBatch()
	if err != nil {
		return err
	}
	if !run.opts.DryRun && len(run.batch) > 0 {
		if err := verifyImportFileUnchanged(run.session.input, run.session.inputInfo); err != nil {
			return err
		}
		response, err := sendFrontierImportBatch(
			run.ctx, run.client, run.session.endpoint.String(), run.opts.APIKey, importBatch,
		)
		if err != nil {
			return err
		}
		run.session.state.EnqueuedRequests += int64(response.EnqueuedRequests)
		run.session.state.AlreadyIndexed += int64(response.AlreadyIndexed)
	}
	run.recordFlushedBatch()
	if run.opts.DryRun {
		return nil
	}
	if err := saveCheckpoint(run.session.statePath, run.session.state); err != nil {
		return err
	}
	if err := writeFrontierImportProgress(run.progress, run.session.state); err != nil {
		return err
	}
	return nil
}

func (run *frontierBatchRun) mappedBatch() (yagocrawlcontract.CrawlOrderImportBatch, error) {
	if len(run.batch) == 0 {
		return yagocrawlcontract.CrawlOrderImportBatch{}, nil
	}
	batch := makeFrontierImportBatch(
		run.session.state.SourceID, run.profileHandle,
		run.session.profiles, run.batch, false,
	)
	if err := batch.Validate(); err != nil {
		return yagocrawlcontract.CrawlOrderImportBatch{},
			fmt.Errorf("validate mapped frontier batch at byte %d: %w", run.batchEnd, err)
	}
	encoded, _ := json.Marshal(batch)
	if len(encoded) > yagocrawlcontract.MaximumCrawlOrderImportBatchBytes {
		return yagocrawlcontract.CrawlOrderImportBatch{}, fmt.Errorf(
			"mapped frontier batch exceeds the %d-byte limit; rerun with a smaller --batch-records value",
			yagocrawlcontract.MaximumCrawlOrderImportBatchBytes,
		)
	}
	return batch, nil
}

func (run *frontierBatchRun) recordFlushedBatch() {
	state := &run.session.state
	state.Offset = run.batchEnd
	state.Records += run.batchRows
	state.UniqueRequests += int64(len(run.batch))
	state.DuplicateRequests += run.duplicates
	state.BlockedCredentialURLs += run.blockedCreds
	state.BlockedInvalidURLs += run.blockedInvalid
	run.batch = nil
	run.batchRows = 0
	run.duplicates = 0
	run.blockedCreds = 0
	run.blockedInvalid = 0
	run.profileHandle = ""
}

func mapYaCyFrontierRequest(
	row yaCyFrontierRecord,
	profile yagocrawlcontract.CrawlProfile,
) (yagocrawlcontract.CrawlRequest, error) {
	if row.Depth < 0 || row.Depth > yagocrawlcontract.MaxCrawlDepth ||
		len(row.Anchor) > yagocrawlcontract.MaximumCrawlOrderImportAnchorBytes {
		return yagocrawlcontract.CrawlRequest{}, errors.New(
			"depth or anchor exceeds migration limit",
		)
	}
	request := yagocrawlcontract.CrawlRequest{
		URL: row.URL, Mode: yagocrawlcontract.CrawlRequestModeURL,
		AnchorName: row.Anchor, Depth: row.Depth, ProfileHandle: profile.Handle,
		SourceMetadata: yagocrawlcontract.NewCrawlSourceMetadata(map[string]string{
			"yacy.source_profile_handle": row.ProfileHandle,
			"yacy.stack":                 row.Stack,
			"yacy.queued_at_millis":      strconv.FormatInt(row.QueuedAtMillis, 10),
			"yacy.initiator_hex":         row.InitiatorHex,
		}),
	}
	if row.QueuedAtMillis > 0 {
		request.AppDate = time.UnixMilli(row.QueuedAtMillis).UTC()
	}
	if decoded, err := hex.DecodeString(row.InitiatorHex); err == nil {
		if initiator, err := yagocrawlcontract.ParseCrawlInitiator(decoded); err == nil {
			request.Initiator = initiator
		}
	}
	return request, nil
}

func frontierURLBlockReason(raw string) string {
	if raw == "" || len(raw) > yagocrawlcontract.MaximumCrawlURLBytes || !utf8.ValidString(raw) {
		return "invalid"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Fragment != "" {
		return "invalid"
	}
	if parsed.User != nil {
		return "credential"
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ftp", "smb":
		return ""
	default:
		return "invalid"
	}
}

func makeFrontierImportBatch(
	sourceID string,
	sourceProfileHandle string,
	profiles map[string]frontierSourceProfile,
	requests []yagocrawlcontract.CrawlRequest,
	dryRun bool,
) yagocrawlcontract.CrawlOrderImportBatch {
	profile := profiles[sourceProfileHandle].profile
	batch := yagocrawlcontract.CrawlOrderImportBatch{
		Version: 1, SourceID: sourceID, Profile: profile,
		Requests: requests, DryRun: dryRun,
	}
	batch.BatchID, _ = batch.ComputeBatchID()
	return batch
}

func sendFrontierImportBatch(
	ctx context.Context,
	client *http.Client,
	endpoint, apiKey string,
	batch yagocrawlcontract.CrawlOrderImportBatch,
) (frontierImportResponse, error) {
	encoded, _ := json.Marshal(batch)
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(encoded),
	)
	if err != nil {
		return frontierImportResponse{}, fmt.Errorf("create frontier import request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := client.Do(request)
	if err != nil {
		return frontierImportResponse{}, fmt.Errorf("send frontier import batch: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return frontierImportResponse{}, fmt.Errorf(
			"frontier import endpoint returned HTTP %d",
			response.StatusCode,
		)
	}
	var result frontierImportResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return frontierImportResponse{}, fmt.Errorf("decode frontier import receipt: %w", err)
	}
	return result, nil
}

func rebuildFrontierSeen(
	input *os.File,
	end int64,
	profiles map[string]frontierSourceProfile,
) (map[string]struct{}, error) {
	return rebuildFrontierSeenWithLimit(input, end, profiles, maximumFrontierSeenURLs)
}

func rebuildFrontierSeenWithLimit(
	input *os.File,
	end int64,
	profiles map[string]frontierSourceProfile,
	maximum int,
) (map[string]struct{}, error) {
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek YaCy frontier for checkpoint recovery: %w", err)
	}
	reader := bufio.NewReaderSize(input, 64<<10)
	_, headerEnd, err := readFrontierHeader(reader)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	position := headerEnd
	for position < end {
		line, err := readBoundedLine(reader)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read saved frontier before checkpoint: %w", err)
		}
		if len(line) == 0 {
			return nil, errors.New("checkpoint falls beyond frontier data")
		}
		position += int64(len(line))
		if position > end {
			return nil, errors.New("frontier checkpoint is not at a record boundary")
		}
		var row yaCyFrontierRecord
		if json.Unmarshal(line, &row) != nil || row.Type != "crawlRequest" || row.Version != 1 ||
			row.Stack != "LOCAL" {
			return nil, errors.New("invalid YaCy frontier record before checkpoint")
		}
		if _, ok := profiles[row.ProfileHandle]; !ok {
			return nil, errors.New("saved frontier references a missing YaCy profile")
		}
		identity := row.ProfileHandle + "\x00" + row.URL
		if _, err := addFrontierSeen(seen, identity, maximum); err != nil {
			return nil, err
		}
	}
	if position != end {
		return nil, errors.New("frontier checkpoint is not at a record boundary")
	}
	return seen, nil
}

type frontierCheckpointValidation struct {
	state        frontierImportCheckpoint
	found        bool
	inputInfo    os.FileInfo
	profileInfo  os.FileInfo
	inputHash    string
	profilesHash string
	sourceID     string
	endpoint     *url.URL
	dryRun       bool
}

func validateFrontierCheckpoint(check frontierCheckpointValidation) error {
	if check.dryRun {
		return nil
	}
	if !check.found {
		return nil
	}
	if check.state.Version != 1 || check.state.SourceID != check.sourceID ||
		check.state.InputSHA256 != check.inputHash || check.state.ProfilesSHA256 != check.profilesHash ||
		check.state.InputSize != check.inputInfo.Size() ||
		check.state.InputModTime != check.inputInfo.ModTime().UnixNano() ||
		check.state.ProfilesSize != check.profileInfo.Size() ||
		check.state.ProfilesModTime != check.profileInfo.ModTime().UnixNano() ||
		check.state.Offset < 0 || check.state.Offset > check.inputInfo.Size() || check.endpoint == nil ||
		check.state.DestinationFingerprint != destinationFingerprint(check.endpoint) {
		return errors.New("frontier checkpoint does not match the input files or destination")
	}
	return nil
}

func loadFrontierImportCheckpoint(path string) (frontierImportCheckpoint, bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- selected by local migration operator.
	if errors.Is(err, os.ErrNotExist) {
		return frontierImportCheckpoint{}, false, nil
	}
	if err != nil {
		return frontierImportCheckpoint{}, false, fmt.Errorf("read frontier checkpoint: %w", err)
	}
	var state frontierImportCheckpoint
	if json.Unmarshal(data, &state) != nil || state.Version != 1 || state.SourceID == "" ||
		state.Offset < 0 {
		return frontierImportCheckpoint{}, false, errors.New("invalid frontier import checkpoint")
	}
	return state, true, nil
}

func hashOpenFile(file io.ReadSeeker) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek YaCy import file for fingerprint: %w", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("read YaCy import file for fingerprint: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind YaCy import file after fingerprint: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeFrontierImportProgress(progress io.Writer, state frontierImportCheckpoint) error {
	_, err := fmt.Fprintf(
		progress,
		"frontier import checkpoint: records=%d unique=%d enqueued=%d already-indexed=%d duplicates=%d blocked-credential-urls=%d blocked-invalid-urls=%d offset=%d\n",
		state.Records,
		state.UniqueRequests,
		state.EnqueuedRequests,
		state.AlreadyIndexed,
		state.DuplicateRequests,
		state.BlockedCredentialURLs,
		state.BlockedInvalidURLs,
		state.Offset,
	)
	if err != nil {
		return fmt.Errorf("write frontier import progress: %w", err)
	}
	return nil
}

func writeFrontierImportSummary(
	progress io.Writer,
	state frontierImportCheckpoint,
	dryRun bool,
) error {
	if dryRun {
		_, err := fmt.Fprintf(
			progress,
			"dry-run: validated %d queue records; unique=%d duplicates=%d blocked-credential-urls=%d blocked-invalid-urls=%d; no API calls or checkpoint changes\n",
			state.Records,
			state.UniqueRequests,
			state.DuplicateRequests,
			state.BlockedCredentialURLs,
			state.BlockedInvalidURLs,
		)
		if err != nil {
			return fmt.Errorf("write frontier import summary: %w", err)
		}
		return nil
	}
	return writeFrontierImportProgress(progress, state)
}
