package frontiercheckpoint

import "github.com/D4rk4/yago/yagocrawlcontract"

func sameConditionalFetch(left, right *yagocrawlcontract.ConditionalFetch) bool {
	if left == nil || right == nil {
		return left == right
	}

	return left.Validators == right.Validators && left.Revision.Matches(right.Revision)
}
