package crawltraversal

import "context"

type AlwaysDue struct{}

func (AlwaysDue) Due(context.Context, string) (bool, error) {
	return true, nil
}
