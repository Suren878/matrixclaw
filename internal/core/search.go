package core

import (
	"context"
	"fmt"
	"strings"
)

const defaultSearchLimit = 20

func (c *Core) Search(ctx context.Context, filter SearchFilter) (SearchReport, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	if filter.Query == "" {
		return SearchReport{}, fmt.Errorf("%w: search query is required", ErrInvalidInput)
	}
	if filter.Limit <= 0 {
		filter.Limit = defaultSearchLimit
	}
	results, err := c.store.SearchMessages(ctx, filter)
	if err != nil {
		return SearchReport{}, err
	}
	return SearchReport{Query: filter.Query, Results: results}, nil
}
