package crawlresults

import (
	"context"
	"time"
)

type sourceModifiedFetchRecorder interface {
	RecordFetchWithSourceModified(
		context.Context,
		string,
		string,
		time.Time,
		time.Time,
	) error
}

type sourceModifiedFetchBatchRecorder interface {
	RecordFetchesWithSourceModified(
		context.Context,
		[]string,
		[]string,
		[]time.Time,
		[]time.Time,
	) error
}
