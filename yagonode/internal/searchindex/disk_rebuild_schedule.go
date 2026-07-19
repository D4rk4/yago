package searchindex

import (
	"fmt"
	"strings"
)

func DiskRebuildPending(root string) (bool, error) {
	if strings.TrimSpace(root) == "" {
		return false, fmt.Errorf("search index path is empty")
	}

	return bleveRebuildPending(root)
}

func ScheduleDiskRebuild(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("search index path is empty")
	}

	return requireBleveRebuild(root)
}
