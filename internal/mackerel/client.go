// Package mackerel is a thin Mackerel API client on top of httpx.
//
// It works on raw JSON so that fields unknown to mcrl are never dropped
// (docs/design/03-architecture.md). Typed access is limited to what mcrl itself
// needs: IDs and names for resolution, and filter fields.
package mackerel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/kga/mcrl/internal/httpx"
)

// Client calls the Mackerel API.
type Client struct {
	HTTP *httpx.Client
}

// New wraps an httpx client.
func New(h *httpx.Client) *Client { return &Client{HTTP: h} }

// Get sends a GET request and returns the body.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	resp, err := c.HTTP.Do(ctx, &httpx.Request{Method: http.MethodGet, Path: path, Query: query})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// List gets path and returns the array under key.
func (c *Client) List(ctx context.Context, path, key string, query url.Values) ([]json.RawMessage, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	raw, err := Field(body, key)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("GET %s: %q is not an array", path, key)
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	return items, nil
}

// Field returns the value of key in a JSON object.
func Field(body []byte, key string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	v, ok := obj[key]
	if !ok {
		return nil, fmt.Errorf("unexpected response: no %q field", key)
	}
	return v, nil
}

// UnwrapIf returns body[key] when body is an object having key, else body.
// Some single-resource endpoints wrap the resource ({"monitor": {...}}) and
// some do not.
func UnwrapIf(body []byte, key string) json.RawMessage {
	if key == "" {
		return body
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) == nil {
		if v, ok := obj[key]; ok && len(obj) == 1 {
			return v
		}
	}
	return body
}

// IsNull reports whether raw is the JSON null literal.
func IsNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// NotFoundError means no resource matched a reference.
type NotFoundError struct {
	Kind string
	Ref  string
}

func (e *NotFoundError) Error() string { return fmt.Sprintf("%s %q not found", e.Kind, e.Ref) }

// Candidate is one of several resources that matched a reference.
type Candidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AmbiguousError means several resources matched a reference.
type AmbiguousError struct {
	Kind       string
	Ref        string
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	ids := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		ids[i] = c.ID
	}
	return fmt.Sprintf("%s %q matches %d resources (%s); specify the ID", e.Kind, e.Ref, len(e.Candidates), strings.Join(ids, ", "))
}

// AsNotFound converts the 400/404 of a GET by ID into NotFoundError. The API
// returns 400 instead of 404 for IDs that do not decode (docs/design/api-notes.md).
func AsNotFound(err error, kind, ref string) error {
	var ae *httpx.APIError
	if errors.As(err, &ae) && (ae.StatusCode == http.StatusNotFound || ae.StatusCode == http.StatusBadRequest) {
		return &NotFoundError{Kind: kind, Ref: ref}
	}
	return err
}

// decodeObject decodes a JSON object into a generic map.
func decodeObject(raw json.RawMessage) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// StringField returns obj[key] if it is a string.
func StringField(raw json.RawMessage, key string) string {
	s, _ := decodeObject(raw)[key].(string)
	return s
}
