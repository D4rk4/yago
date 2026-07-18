package frontiercheckpoint

import (
	"encoding/binary"
	"fmt"
)

type outstandingPageRow struct {
	key  []byte
	page Page
}

func findOutstandingPage(
	buckets checkpointBuckets,
	prefix []byte,
	pageURL string,
) (outstandingPageRow, bool, error) {
	position := buckets.pagePositions.Get(childRowKey(prefix, pageURL))
	if position == nil {
		return outstandingPageRow{}, false, nil
	}
	if len(position) != 8 {
		return outstandingPageRow{}, false, fmt.Errorf(
			"%w: invalid page position",
			ErrCorruptCheckpoint,
		)
	}
	pageKey := sequenceRowKey(prefix, binary.BigEndian.Uint64(position))
	encodedPage := buckets.pages.Get(pageKey)
	if encodedPage == nil {
		return outstandingPageRow{}, false, fmt.Errorf(
			"%w: outstanding page is missing",
			ErrCorruptCheckpoint,
		)
	}
	var page Page
	if err := decodeRow("page", encodedPage, &page); err != nil {
		return outstandingPageRow{}, false, err
	}
	if page.URL != pageURL {
		return outstandingPageRow{}, false, fmt.Errorf(
			"%w: outstanding page identity mismatch",
			ErrCorruptCheckpoint,
		)
	}
	return outstandingPageRow{key: pageKey, page: page}, true, nil
}
