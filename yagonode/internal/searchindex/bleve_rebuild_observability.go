package searchindex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	bleveRebuildStartedMessage   = "bleve index rebuild started"
	bleveRebuildProgressMessage  = "bleve index rebuild progress"
	bleveRebuildCompletedMessage = "bleve index rebuild completed"
	bleveRebuildMilestones       = 10
)

type BleveRebuildStorageObservation struct {
	AvailableBytes       uint64
	ReservedBytes        uint64
	MeasurementAvailable bool
}

type bleveRebuildHeadroomAdmission interface {
	CheckGrowthWithHeadroom(uint64) error
}

type bleveRebuildStorageObservationSource interface {
	RebuildStorageObservation() BleveRebuildStorageObservation
}

type bleveRebuildFootprintFilesystem struct {
	inspect func(string) (fs.FileInfo, error)
	walk    func(string, fs.WalkDirFunc) error
}

type bleveRebuildCoordinator struct {
	root               string
	documents          documentstore.DocumentDirectory
	admission          BleveRebuildGrowthAdmission
	now                func() time.Time
	documentsTotal     int
	documentsIndexed   int
	batches            int
	milestone          int
	startedAt          time.Time
	prepared           bool
	estimatedBytes     uint64
	estimateAvailable  bool
	storageObservation BleveRebuildStorageObservation
}

func newBleveRebuildCoordinator(
	root string,
	documents documentstore.DocumentDirectory,
	admission BleveRebuildGrowthAdmission,
) *bleveRebuildCoordinator {
	return &bleveRebuildCoordinator{
		root: root, documents: documents, admission: admission, now: time.Now,
	}
}

func (c *bleveRebuildCoordinator) prepare(ctx context.Context) error {
	if c.prepared {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bleve rebuild preflight context: %w", err)
	}
	documentsTotal, err := c.documents.Count(ctx)
	if err != nil {
		return fmt.Errorf("count bleve rebuild documents: %w", err)
	}
	estimatedBytes, estimateAvailable, err := bleveRebuildFootprint(c.root)
	if err != nil {
		return fmt.Errorf("measure bleve rebuild footprint: %w", err)
	}
	observation := BleveRebuildStorageObservation{}
	if source, ok := c.admission.(bleveRebuildStorageObservationSource); ok {
		observation = source.RebuildStorageObservation()
	}
	if admission, ok := c.admission.(bleveRebuildHeadroomAdmission); ok && estimateAvailable {
		if err := admission.CheckGrowthWithHeadroom(estimatedBytes); err != nil {
			return fmt.Errorf("check bleve rebuild storage headroom: %w", err)
		}
	} else if c.admission != nil {
		if err := c.admission.CheckGrowth(); err != nil {
			return fmt.Errorf("check bleve rebuild storage growth: %w", err)
		}
	}
	c.documentsTotal = documentsTotal
	c.estimatedBytes = estimatedBytes
	c.estimateAvailable = estimateAvailable
	c.storageObservation = observation
	c.startedAt = c.now()
	c.prepared = true
	slog.InfoContext(ctx, bleveRebuildStartedMessage,
		slog.Int("documentsTotal", documentsTotal),
		slog.Bool("estimateAvailable", estimateAvailable),
		slog.Uint64("estimatedRebuildBytes", estimatedBytes),
		slog.Bool("storageMeasurementAvailable", observation.MeasurementAvailable),
		slog.Uint64("storageAvailableBytes", observation.AvailableBytes),
		slog.Uint64("storageReservedBytes", observation.ReservedBytes),
		slog.Uint64("storageHeadroomBytes", rebuildStorageHeadroom(observation)),
	)

	return nil
}

func (c *bleveRebuildCoordinator) CheckGrowth() error {
	if c.admission == nil {
		return nil
	}
	if err := c.admission.CheckGrowth(); err != nil {
		return fmt.Errorf("check bleve rebuild storage growth: %w", err)
	}

	return nil
}

func (c *bleveRebuildCoordinator) BleveRebuildBatchIndexed(documents int) {
	if documents <= 0 {
		return
	}
	c.documentsIndexed += documents
	c.batches++
	if c.documentsTotal <= 0 {
		return
	}
	milestone := bleveRebuildMilestone(c.documentsIndexed, c.documentsTotal)
	if milestone <= c.milestone || milestone >= bleveRebuildMilestones {
		return
	}
	c.milestone = milestone
	slog.InfoContext(context.Background(), bleveRebuildProgressMessage,
		slog.Int("documentsIndexed", c.documentsIndexed),
		slog.Int("documentsTotal", c.documentsTotal),
		slog.Int("percent", milestone*100/bleveRebuildMilestones),
	)
}

func bleveRebuildMilestone(indexed, total int) int {
	if total <= 0 || indexed <= 0 {
		return 0
	}
	if indexed >= total {
		return bleveRebuildMilestones
	}
	for milestone := bleveRebuildMilestones - 1; milestone > 0; milestone-- {
		threshold := total/bleveRebuildMilestones*milestone +
			(total%bleveRebuildMilestones*milestone+bleveRebuildMilestones-1)/bleveRebuildMilestones
		if indexed >= threshold {
			return milestone
		}
	}

	return 0
}

func (c *bleveRebuildCoordinator) complete(ctx context.Context) {
	duration := c.now().Sub(c.startedAt)
	if duration < 0 {
		duration = 0
	}
	slog.InfoContext(ctx, bleveRebuildCompletedMessage,
		slog.Int("documentsIndexed", c.documentsIndexed),
		slog.Int("documentsTotal", c.documentsTotal),
		slog.Int("batches", c.batches),
		slog.Int64("durationMilliseconds", duration.Milliseconds()),
	)
}

func rebuildStorageHeadroom(observation BleveRebuildStorageObservation) uint64 {
	if !observation.MeasurementAvailable ||
		observation.AvailableBytes <= observation.ReservedBytes {
		return 0
	}

	return observation.AvailableBytes - observation.ReservedBytes
}

func bleveRebuildFootprint(root string) (uint64, bool, error) {
	return bleveRebuildFootprintWithFilesystem(root, bleveRebuildFootprintFilesystem{
		inspect: os.Stat,
		walk:    filepath.WalkDir,
	})
}

func bleveRebuildFootprintWithFilesystem(
	root string,
	filesystem bleveRebuildFootprintFilesystem,
) (uint64, bool, error) {
	info, err := filesystem.inspect(root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("inspect bleve rebuild footprint: %w", err)
	}
	if !info.IsDir() {
		return regularFileBytes(info), true, nil
	}
	var total uint64
	err = filesystem.walk(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("visit bleve rebuild footprint %q: %w", path, walkErr)
		}
		if entry.IsDir() {
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("inspect bleve rebuild footprint entry %q: %w", path, infoErr)
		}
		bytes := regularFileBytes(entryInfo)
		if bytes > math.MaxUint64-total {
			total = math.MaxUint64

			return fs.SkipAll
		}
		total += bytes

		return nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("walk bleve rebuild footprint: %w", err)
	}

	return total, true, nil
}

func regularFileBytes(info fs.FileInfo) uint64 {
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return 0
	}

	bytes, _ := strconv.ParseUint(strconv.FormatInt(info.Size(), 10), 10, 64)

	return bytes
}
