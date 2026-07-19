package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestOpenNodeStoragePassesSearchRebuildAdmission(t *testing.T) {
	restoreStorageSeams(t)
	admission := &nodeGrowthAdmission{}
	var observed searchindex.BleveRebuildGrowthAdmission
	openSearchIndex = func(
		_ context.Context,
		_ string,
		_ documentstore.DocumentDirectory,
		admissions ...searchindex.BleveRebuildGrowthAdmission,
	) (searchindex.SearchIndex, error) {
		if len(admissions) > 0 {
			observed = admissions[0]
		}

		return stubSearchIndex{}, nil
	}
	storage, err := openNodeStorage(openTestVault(t), "search", admission)
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	if observed == nil || storage.searchIndex == nil {
		t.Fatalf("rebuild admission = %T, search index = %T", observed, storage.searchIndex)
	}
	if err := observed.CheckGrowth(); err != nil || admission.calls != 1 {
		t.Fatalf("rebuild growth check = %v, calls = %d", err, admission.calls)
	}
	headroom, ok := observed.(interface{ CheckGrowthWithHeadroom(uint64) error })
	if !ok {
		t.Fatalf("rebuild admission lacks headroom check: %T", observed)
	}
	if err := headroom.CheckGrowthWithHeadroom(17); err != nil ||
		admission.requiredHeadroom != 17 {
		t.Fatalf("rebuild headroom check = %v, required = %d", err, admission.requiredHeadroom)
	}
}
