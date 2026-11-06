package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError is a non-2xx response from the Mackerel API.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	// Message is taken from {"error":{"message":"..."}} or the raw body.
	Message string
	Body    []byte
	Header  http.Header
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("%s (HTTP %d %s %s)", msg, e.StatusCode, e.Method, e.Path)
}

func newAPIError(method, path string, resp *Response) *APIError {
	return &APIError{
		StatusCode: resp.StatusCode,
		Method:     method,
		Path:       path,
		Message:    errorMessage(resp.Body),
		Body:       resp.Body,
		Header:     resp.Header,
	}
}

// errorMessage extracts the message from an error body. The documented-by-
// observation shape is {"error":{"message":"..."}}; other shapes fall back
// to the first line of the body.
func errorMessage(body []byte) string {
	var v struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &v) == nil && len(v.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(v.Error, &obj) == nil && obj.Message != "" {
			return obj.Message
		}
		var s string
		if json.Unmarshal(v.Error, &s) == nil && s != "" {
			return s
		}
	}
	s := strings.TrimSpace(string(body))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}
