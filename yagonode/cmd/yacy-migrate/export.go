package main

import (
	"bufio"
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
)

const maximumSolrRows = 256

const solrStoredFields = "id,sku,canonical_s,title,description_txt,text_t,h1_txt,h2_txt,h3_txt,h4_txt,h5_txt,h6_txt,language_s,content_type,httpstatus_i,failreason_s,failtype_s,load_date_dt,last_modified,fresh_date_dt,exact_signature_l,collection_sxt,focused_policy_sxt,focused_profile_version_sxt,focused_relevance_i,focused_priority_s,focused_reason_sxt,httpstatus_redirect_s,outboundlinks_protocol_sxt,outboundlinks_urlstub_sxt,inboundlinks_protocol_sxt,inboundlinks_urlstub_sxt"

type exportCheckpoint struct {
	Version           int    `json:"version"`
	SourceID          string `json:"sourceId"`
	SourceFingerprint string `json:"sourceFingerprint"`
	Cursor            string `json:"cursor"`
	Offset            int64  `json:"offset"`
	Records           int64  `json:"records"`
	Complete          bool   `json:"complete"`
}

type solrPage struct {
	Response struct {
		NumFound int                          `json:"numFound"`
		Docs     []map[string]json.RawMessage `json:"docs"`
	} `json:"response"`
	NextCursorMark string `json:"nextCursorMark"`
}

type exportOptions struct {
	SolrURL  string
	Output   string
	SourceID string
	User     string
	Password string
	Rows     int
	DryRun   bool
}

type exportFile interface {
	io.Writer
	Chmod(os.FileMode) error
	Stat() (os.FileInfo, error)
	Truncate(int64) error
	Seek(int64, int) (int64, error)
	Sync() error
	Close() error
}

type checkpointTemporaryFile interface {
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type checkpointDirectory interface {
	Sync() error
	Close() error
}

func exportCorpus(ctx context.Context, opts exportOptions, progress io.Writer) (resultErr error) {
	endpoint, err := validateExportOptions(opts)
	if err != nil {
		return err
	}
	reader := newSolrReader(endpoint, opts)
	if opts.DryRun {
		return runExportDryRun(ctx, reader, progress)
	}
	session, complete, err := startExport(opts, endpoint, progress)
	if err != nil {
		return err
	}
	if complete {
		return nil
	}
	defer func() {
		resultErr = closeExportOutput(session.output, resultErr)
	}()
	return runExportSession(ctx, session, opts, reader, progress)
}

func runExportSession(
	ctx context.Context,
	session *exportSession,
	opts exportOptions,
	reader *solrReader,
	progress io.Writer,
) error {
	if session == nil {
		return errors.New("export session was not initialized")
	}
	if !session.resumed {
		if err := session.writeHeader(opts.SourceID); err != nil {
			return err
		}
	}
	return session.exportPages(ctx, opts, reader, progress)
}

func closeExportOutput(output exportFile, resultErr error) error {
	if err := output.Close(); err != nil {
		return errors.Join(resultErr, fmt.Errorf("close export output: %w", err))
	}
	return resultErr
}

func validateExportOptions(opts exportOptions) (*url.URL, error) {
	endpoint, err := url.Parse(opts.SolrURL)
	if err != nil || endpoint.Host == "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.Fragment != "" {
		return nil, errors.New(
			"solr URL must be an absolute HTTP(S) endpoint without embedded credentials",
		)
	}
	if strings.TrimSpace(opts.SourceID) == "" || len(opts.SourceID) > 128 || opts.Rows < 1 ||
		opts.Rows > maximumSolrRows {
		return nil, errors.New("source ID is required and rows must be between 1 and 256")
	}
	if !opts.DryRun && opts.Output == "" {
		return nil, errors.New("output path is required")
	}
	return endpoint, nil
}

func runExportDryRun(ctx context.Context, reader *solrReader, progress io.Writer) error {
	page, err := reader.read(ctx, "*", 0)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		progress,
		"dry-run: source reports %d indexed rows; no output was written\n",
		page.Response.NumFound,
	); err != nil {
		return fmt.Errorf("write export progress: %w", err)
	}
	return nil
}

func startExport(
	opts exportOptions,
	endpoint *url.URL,
	progress io.Writer,
) (*exportSession, bool, error) {
	statePath := opts.Output + ".state.json"
	state, found, err := loadExportCheckpoint(statePath)
	if err != nil {
		return nil, false, err
	}
	if err := validateExportCheckpoint(state, found, opts, endpoint); err != nil {
		return nil, false, err
	}
	if found && state.Complete {
		if _, err := fmt.Fprintf(
			progress,
			"export already complete: %d records\n",
			state.Records,
		); err != nil {
			return nil, false, fmt.Errorf("write export progress: %w", err)
		}
		return nil, true, nil
	}
	output, err := openExportFile(opts.Output, found, state.Offset)
	if err != nil {
		return nil, false, err
	}
	return &exportSession{
		output:            output,
		writer:            bufio.NewWriterSize(output, 1<<20),
		statePath:         statePath,
		state:             state,
		sourceFingerprint: endpointFingerprint(endpoint),
		resumed:           found,
	}, false, nil
}

func validateExportCheckpoint(
	state exportCheckpoint,
	found bool,
	opts exportOptions,
	endpoint *url.URL,
) error {
	if !found {
		return nil
	}
	if state.SourceID != opts.SourceID {
		return errors.New("export checkpoint belongs to a different source ID")
	}
	if state.SourceFingerprint != endpointFingerprint(endpoint) {
		return errors.New("export checkpoint belongs to a different Solr endpoint")
	}
	return nil
}

type exportSession struct {
	output            exportFile
	writer            *bufio.Writer
	statePath         string
	state             exportCheckpoint
	sourceFingerprint string
	resumed           bool
}

func (session *exportSession) writeHeader(sourceID string) error {
	session.state = exportCheckpoint{
		Version: 1, SourceID: sourceID,
		SourceFingerprint: session.sourceFingerprint,
		Cursor:            "*",
	}
	header := interchangeHeader{Type: "header", Version: 1, SourceID: sourceID}
	if err := writeJSONLine(session.writer, header); err != nil {
		return err
	}
	if err := session.writer.Flush(); err != nil {
		return fmt.Errorf("flush export header: %w", err)
	}
	if err := session.output.Sync(); err != nil {
		return fmt.Errorf("sync export header: %w", err)
	}
	offset, err := session.output.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("measure export header offset: %w", err)
	}
	session.state.Offset = offset
	if err := saveCheckpoint(session.statePath, session.state); err != nil {
		return err
	}
	return nil
}

func (session *exportSession) exportPages(
	ctx context.Context,
	opts exportOptions,
	reader *solrReader,
	progress io.Writer,
) error {
	for {
		page, err := reader.read(ctx, session.state.Cursor, opts.Rows)
		if err != nil {
			return fmt.Errorf("read Solr cursor %q: %w", session.state.Cursor, err)
		}
		if err := session.writeDocuments(page, opts.SourceID); err != nil {
			return err
		}
		finished, err := session.commitPage(page, progress)
		if err != nil {
			return err
		}
		if finished {
			return nil
		}
	}
}

func (session *exportSession) writeDocuments(page solrPage, sourceID string) error {
	for _, raw := range page.Response.Docs {
		record, err := mapSolrDocument(raw, sourceID)
		if err != nil {
			return fmt.Errorf("map source row at record %d: %w", session.state.Records+1, err)
		}
		if err := writeJSONLine(
			session.writer,
			interchangeRecord{Type: "document", Version: 1, Record: record},
		); err != nil {
			return err
		}
		session.state.Records++
	}
	return nil
}

func (session *exportSession) commitPage(page solrPage, progress io.Writer) (bool, error) {
	if page.NextCursorMark == "" {
		return false, errors.New("solr response did not include nextCursorMark")
	}
	if err := session.writer.Flush(); err != nil {
		return false, fmt.Errorf("flush exported Solr page: %w", err)
	}
	if err := session.output.Sync(); err != nil {
		return false, fmt.Errorf("sync exported Solr page: %w", err)
	}
	offset, err := session.output.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, fmt.Errorf("measure exported Solr page offset: %w", err)
	}
	finished := page.NextCursorMark == session.state.Cursor
	session.state.Offset = offset
	session.state.Cursor = page.NextCursorMark
	session.state.Complete = finished
	if err := saveCheckpoint(session.statePath, session.state); err != nil {
		return false, err
	}
	if _, err := fmt.Fprintf(
		progress,
		"exported %d source rows\n",
		session.state.Records,
	); err != nil {
		return false, fmt.Errorf("write export progress: %w", err)
	}
	return finished, nil
}

func endpointFingerprint(endpoint *url.URL) string {
	sum := sha256.Sum256([]byte(endpoint.String()))
	return hex.EncodeToString(sum[:])
}

type solrReader struct {
	client   *http.Client
	endpoint *url.URL
	user     string
	password string
}

func newSolrReader(endpoint *url.URL, opts exportOptions) *solrReader {
	return &solrReader{
		client:   &http.Client{Timeout: 2 * time.Minute},
		endpoint: endpoint,
		user:     opts.User,
		password: opts.Password,
	}
}

func (reader *solrReader) read(
	ctx context.Context,
	cursor string,
	rows int,
) (page solrPage, resultErr error) {
	query := reader.endpoint.Query()
	query.Set("q", "*:*")
	query.Set("wt", "json")
	query.Set("fl", solrStoredFields)
	query.Set("rows", strconv.Itoa(rows))
	query.Set("sort", "id asc")
	query.Set("cursorMark", cursor)
	endpointCopy := *reader.endpoint
	endpointCopy.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointCopy.String(), nil)
	if err != nil {
		return solrPage{}, fmt.Errorf("build solr request: %w", err)
	}
	if reader.user != "" {
		request.SetBasicAuth(reader.user, reader.password)
	}
	response, err := reader.client.Do(request)
	if err != nil {
		return solrPage{}, fmt.Errorf("request Solr page: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close Solr response: %w", err))
		}
	}()
	if response.StatusCode != http.StatusOK {
		return solrPage{}, fmt.Errorf("solr returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 80<<20))
	if err := decoder.Decode(&page); err != nil {
		return solrPage{}, fmt.Errorf("decode Solr page: %w", err)
	}
	return page, nil
}

func writeJSONLine(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write interchange record: %w", err)
	}
	return nil
}

func openExportFile(path string, hasCheckpoint bool, offset int64) (exportFile, error) {
	flags := os.O_CREATE | os.O_RDWR
	if !hasCheckpoint {
		if _, err := os.Stat(path); err == nil {
			return nil, errors.New(
				"output already exists without a checkpoint; choose a new path or remove the incomplete file",
			)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect output path: %w", err)
		}
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(
		path,
		flags,
		0o600,
	) // #nosec G304 -- output path is explicitly selected by the local operator.
	if err != nil {
		return nil, fmt.Errorf("open export output: %w", err)
	}
	if err := prepareExportFile(file, offset); err != nil {
		return nil, closeFileAfterError(file, "prepare export output", err)
	}
	return file, nil
}

func prepareExportFile(file exportFile, offset int64) error {
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict export output permissions: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect export output: %w", err)
	}
	if offset < 0 || offset > info.Size() {
		return errors.New("checkpoint offset is outside the output file")
	}
	if err := file.Truncate(offset); err != nil {
		return fmt.Errorf("truncate export output: %w", err)
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek export output: %w", err)
	}
	return nil
}

func loadExportCheckpoint(path string) (exportCheckpoint, bool, error) {
	data, err := os.ReadFile(
		path,
	) // #nosec G304 -- checkpoint path is explicitly selected by the local operator.
	if errors.Is(err, os.ErrNotExist) {
		return exportCheckpoint{}, false, nil
	}
	if err != nil {
		return exportCheckpoint{}, false, fmt.Errorf("read export checkpoint: %w", err)
	}
	var state exportCheckpoint
	if err := json.Unmarshal(data, &state); err != nil || state.Version != 1 || state.Cursor == "" {
		return exportCheckpoint{}, false, errors.New("invalid export checkpoint")
	}
	return state, true, nil
}

func closeFileAfterError(file exportFile, operation string, primary error) error {
	if err := file.Close(); err != nil {
		primary = errors.Join(primary, fmt.Errorf("close file after %s: %w", operation, err))
	}
	return fmt.Errorf("%s: %w", operation, primary)
}

func saveCheckpoint(path string, value any) (resultErr error) {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".yacy-migrate-checkpoint-*")
	if err != nil {
		return fmt.Errorf("create checkpoint temporary file: %w", err)
	}
	return persistCheckpointTemporary(file, file.Name(), path, directory, data)
}

func persistCheckpointTemporary(
	file checkpointTemporaryFile,
	temporary, path, directory string,
	data []byte,
) (resultErr error) {
	defer func() {
		if temporary != "" {
			if err := removeCheckpointTemporary(temporary); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if err := writeCheckpointTemporary(file, data); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace checkpoint: %w", err)
	}
	temporary = ""
	return syncCheckpointDirectory(directory)
}

func removeCheckpointTemporary(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove checkpoint temporary file: %w", err)
	}
	return nil
}

func writeCheckpointTemporary(file checkpointTemporaryFile, data []byte) (resultErr error) {
	defer func() {
		if err := file.Close(); err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("close checkpoint temporary file: %w", err),
			)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict checkpoint permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync checkpoint: %w", err)
	}
	return nil
}

func syncCheckpointDirectory(directory string) error {
	dir, err := os.Open(
		directory,
	) // #nosec G304 -- directory derives from the operator-selected checkpoint path.
	if err != nil {
		return fmt.Errorf("open checkpoint directory for sync: %w", err)
	}
	return syncAndCloseCheckpointDirectory(dir)
}

func syncAndCloseCheckpointDirectory(dir checkpointDirectory) error {
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil || closeErr != nil {
		return errors.Join(
			wrapCheckpointError("sync checkpoint directory", syncErr),
			wrapCheckpointError("close checkpoint directory", closeErr),
		)
	}
	return nil
}

func wrapCheckpointError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
