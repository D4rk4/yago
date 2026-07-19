package shardvault

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenReportsStableSequentialShardAndWordFilterProgress(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vault")
	discarded := startupProgress{
		logger: slog.New(slog.DiscardHandler),
		clock:  time.Now,
	}
	created, err := openEngineWithStartupProgress(
		directory,
		1<<20,
		discarded,
		WithWordFilter(testBucket, testWordWidth),
	)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("close created engine: %v", err)
	}

	var output bytes.Buffer
	clock := intervalStartupClock(time.Unix(100, 0), time.Second)
	progress := startupProgress{
		logger: slog.New(slog.NewJSONHandler(&output, nil)),
		clock:  clock,
	}
	reopened, err := openEngineWithStartupProgress(
		directory,
		1<<20,
		progress,
		WithWordFilter(testBucket, testWordWidth),
	)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}

	records := decodeStartupRecords(t, output.Bytes())
	if len(records) != minShards*2+2 {
		t.Fatalf("records = %d, want %d", len(records), minShards*2+2)
	}
	assertStartupShardRecords(t, records, directory)
	assertStartupWordFilterRecords(t, records[minShards*2:])
}

func assertStartupShardRecords(t *testing.T, records []map[string]any, directory string) {
	t.Helper()
	for shard := range minShards {
		opening := records[shard*2]
		opened := records[shard*2+1]
		path := shardPath(directory, shard)
		assertStartupRecord(t, opening, vaultShardOpeningMessage, shard, path)
		assertStartupRecord(t, opened, vaultShardOpenedMessage, shard, path)
		if opening["completed"] != float64(shard) ||
			opened["completed"] != float64(shard+1) {
			t.Fatalf(
				"shard %d completion = %v/%v",
				shard+1,
				opening["completed"],
				opened["completed"],
			)
		}
		if opening["duration"] != float64(0) {
			t.Fatalf("shard %d opening duration = %v, want 0", shard+1, opening["duration"])
		}
		if opened["duration"] != float64(time.Second) {
			t.Fatalf("shard %d duration = %v, want 1s", shard, opened["duration"])
		}
		openingSize, openingSizePresent := opening["sizeBytes"].(float64)
		openedSize, openedSizePresent := opened["sizeBytes"].(float64)
		if !openingSizePresent || !openedSizePresent || openingSize <= 0 || openedSize <= 0 {
			t.Fatalf(
				"shard %d sizes = %v/%v, want positive",
				shard,
				opening["sizeBytes"],
				opened["sizeBytes"],
			)
		}
	}
}

func assertStartupWordFilterRecords(t *testing.T, records []map[string]any) {
	t.Helper()
	building := records[0]
	built := records[1]
	if building["msg"] != vaultWordFilterBuildingMessage ||
		building["level"] != "INFO" ||
		building["total"] != float64(minShards) ||
		building["completed"] != float64(0) ||
		building["duration"] != float64(0) {
		t.Fatalf("word-filter building record = %#v", building)
	}
	if built["msg"] != vaultWordFilterCompleteMessage ||
		built["level"] != "INFO" ||
		built["total"] != float64(minShards) ||
		built["completed"] != float64(minShards) ||
		built["degradedShards"] != float64(0) ||
		built["duration"] != float64(time.Second) {
		t.Fatalf("word-filter built record = %#v", built)
	}
}

func TestOpenWithoutWordFilterOmitsWordFilterPhase(t *testing.T) {
	var output bytes.Buffer
	progress := startupProgress{
		logger: slog.New(slog.NewJSONHandler(&output, nil)),
		clock:  intervalStartupClock(time.Unix(200, 0), time.Second),
	}
	opened, err := openEngineWithStartupProgress(
		filepath.Join(t.TempDir(), "vault"),
		1<<20,
		progress,
	)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	records := decodeStartupRecords(t, output.Bytes())
	if len(records) != minShards*2 {
		t.Fatalf("records = %d, want %d", len(records), minShards*2)
	}
	if records[0]["sizeBytes"] != float64(0) {
		t.Fatalf("new shard opening size = %v, want 0", records[0]["sizeBytes"])
	}
}

func TestOpenAtPublishesRuntimeStartupProgress(t *testing.T) {
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	legacyPath := filepath.Join(t.TempDir(), "storage.db")
	opened, err := OpenAt(
		legacyPath,
		1<<20,
		WithWordFilter(testBucket, testWordWidth),
	)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close opened vault: %v", err)
	}

	records := decodeStartupRecords(t, output.Bytes())
	if len(records) != minShards*2+2 {
		t.Fatalf("records = %d, want %d", len(records), minShards*2+2)
	}
	assertStartupRecord(
		t,
		records[0],
		vaultShardOpeningMessage,
		0,
		shardPath(legacyPath+".vault", 0),
	)
	if records[minShards*2]["msg"] != vaultWordFilterBuildingMessage ||
		records[minShards*2+1]["msg"] != vaultWordFilterCompleteMessage {
		t.Fatalf("word-filter records = %#v", records[minShards*2:])
	}
}

func TestWordFilterCompletionWarnsAboutDegradedShards(t *testing.T) {
	var output bytes.Buffer
	progress := startupProgress{
		logger: slog.New(slog.NewJSONHandler(&output, nil)),
		clock:  intervalStartupClock(time.Unix(300, 0), time.Second),
	}
	started := progress.wordFilterBuilding(minShards)
	progress.wordFilterInitialized(started, minShards, 2)
	records := decodeStartupRecords(t, output.Bytes())
	completed := records[1]
	if completed["msg"] != "vault word filter initialization completed" ||
		completed["level"] != "WARN" || completed["degradedShards"] != float64(2) {
		t.Fatalf("degraded word-filter record = %#v", completed)
	}
}

func TestQuarantineWarningUsesHumanShardOrdinal(t *testing.T) {
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	path := filepath.Join(t.TempDir(), "000000.vlt")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0xff}, 4096), 0o600); err != nil {
		t.Fatalf("write damaged shard: %v", err)
	}
	database, err := openOrQuarantineShard(path, 0)
	if err != nil {
		t.Fatalf("open quarantined shard: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close replacement shard: %v", err)
	}
	records := decodeStartupRecords(t, output.Bytes())
	if len(records) != 1 || records[0]["msg"] != "vault shard quarantined" ||
		records[0]["level"] != "WARN" || records[0]["shard"] != float64(1) ||
		records[0]["path"] != path {
		t.Fatalf("quarantine record = %#v", records)
	}
}

func intervalStartupClock(start time.Time, interval time.Duration) func() time.Time {
	current := start.Add(-interval)

	return func() time.Time {
		current = current.Add(interval)

		return current
	}
}

func decodeStartupRecords(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	records := make([]map[string]any, 0)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log record: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log records: %v", err)
	}

	return records
}

func assertStartupRecord(
	t *testing.T,
	record map[string]any,
	message string,
	shard int,
	path string,
) {
	t.Helper()
	if record["msg"] != message ||
		record["level"] != "INFO" ||
		record["shard"] != float64(shard+1) ||
		record["total"] != float64(minShards) ||
		record["path"] != path {
		t.Fatalf("startup record = %#v", record)
	}
}
