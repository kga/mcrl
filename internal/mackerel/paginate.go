package mackerel

import (
	"context"
	"encoding/json"
	"iter"
	"net/url"
	"strconv"
)

// PageSize is the maximum "limit" of nextId-paginated endpoints.
const PageSize = 100

// NextIDPages iterates the items of a nextId-paginated endpoint
// (alerts, alert logs, check monitors). pageLimit is sent as "limit".
func (c *Client) NextIDPages(ctx context.Context, path, key string, query url.Values, pageLimit int) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		if pageLimit <= 0 || pageLimit > PageSize {
			pageLimit = PageSize
		}
		q.Set("limit", strconv.Itoa(pageLimit))
		for {
			body, err := c.Get(ctx, path, q)
			if err != nil {
				yield(nil, err)
				return
			}
			var page map[string]json.RawMessage
			if err := json.Unmarshal(body, &page); err != nil {
				yield(nil, err)
				return
			}
			var items []json.RawMessage
			if err := json.Unmarshal(page[key], &items); err != nil {
				yield(nil, err)
				return
			}
			for _, it := range items {
				if !yield(it, nil) {
					return
				}
			}
			var next string
			if raw, ok := page["nextId"]; ok {
				_ = json.Unmarshal(raw, &next)
			}
			if next == "" || len(items) == 0 {
				return
			}
			q.Set("nextId", next)
		}
	}
}
