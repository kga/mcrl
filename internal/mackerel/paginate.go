package mackerel

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kga/mcrl/internal/httpx"
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

// PostJSON sends a POST with a JSON body (Logs and Traces search with POST).
func (c *Client) PostJSON(ctx context.Context, path string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(ctx, &httpx.Request{Method: http.MethodPost, Path: path, Body: b, Idempotent: true})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// CursorPages iterates POST /logs results with pageCursor / pagination.endCursor.
// body is modified to add pageCursor.
func (c *Client) CursorPages(ctx context.Context, path string, body map[string]any) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		for {
			b, err := c.PostJSON(ctx, path, body)
			if err != nil {
				yield(nil, err)
				return
			}
			var page struct {
				Results    []json.RawMessage `json:"results"`
				Pagination struct {
					HasNextPage bool    `json:"hasNextPage"`
					EndCursor   *string `json:"endCursor"`
				} `json:"pagination"`
			}
			if err := json.Unmarshal(b, &page); err != nil {
				yield(nil, err)
				return
			}
			for _, r := range page.Results {
				if !yield(r, nil) {
					return
				}
			}
			if !page.Pagination.HasNextPage || page.Pagination.EndCursor == nil || len(page.Results) == 0 {
				return
			}
			body["pageCursor"] = map[string]string{"direction": "NEXT", "cursor": *page.Pagination.EndCursor}
		}
	}
}

// NumberedPages iterates page/perPage endpoints that answer with
// {"results": [...], "hasNextPage": bool}. fetch gets the 1-based page.
func NumberedPages(fetch func(page int) ([]byte, error)) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		for page := 1; ; page++ {
			b, err := fetch(page)
			if err != nil {
				yield(nil, err)
				return
			}
			var p struct {
				Results     []json.RawMessage `json:"results"`
				HasNextPage bool              `json:"hasNextPage"`
			}
			if err := json.Unmarshal(b, &p); err != nil {
				yield(nil, err)
				return
			}
			for _, r := range p.Results {
				if !yield(r, nil) {
					return
				}
			}
			if !p.HasNextPage || len(p.Results) == 0 {
				return
			}
		}
	}
}
