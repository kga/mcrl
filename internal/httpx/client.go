// Package httpx is the HTTP layer for the Mackerel API: authentication,
// retries, client-side rate limiting and debug logging.
package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APIPrefix is prepended to request paths that do not start with "/api/".
const APIPrefix = "/api/v0"

// Client sends requests to the Mackerel API.
type Client struct {
	BaseURL   *url.URL
	APIKey    string
	UserAgent string
	HTTP      *http.Client

	// Retries is the number of retries after the first attempt.
	Retries int
	// Debug receives a dump of every request and response when non-nil.
	Debug io.Writer
	// Sleep waits between retries. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is used for rate limiting. Tests replace it.
	Now func() time.Time

	limiterMu sync.Mutex
	nextSlot  map[string]time.Time
}

// Request is one API call.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
	// Idempotent marks a POST as safe to retry (e.g. posting metrics).
	Idempotent bool
}

// Response is a fully read API response.
type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

// NetworkError wraps transport-level failures (DNS, TCP, TLS, timeouts).
type NetworkError struct {
	Method string
	URL    string
	Err    error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Method, e.URL, e.Err)
}

func (e *NetworkError) Unwrap() error { return e.Err }

// URL builds the absolute URL for a request path.
func (c *Client) URL(path string, query url.Values) *url.URL {
	u := *c.BaseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + NormalizePath(path)
	u.RawQuery = query.Encode()
	return &u
}

// NormalizePath turns "hosts", "/hosts" or "/api/v0/hosts" into "/api/v0/hosts".
// A query string in path is not allowed; use Request.Query.
func NormalizePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasPrefix(path, "/api/") {
		path = APIPrefix + path
	}
	return path
}

// Do sends the request, retrying transient failures. Non-2xx responses
// are returned as *APIError (with the response still available in it).
func (c *Client) Do(ctx context.Context, req *Request) (*Response, error) {
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	u := c.URL(req.Path, req.Query)
	retryable := req.Idempotent || method == http.MethodGet || method == http.MethodHead ||
		method == http.MethodPut || method == http.MethodDelete

	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := c.waitRateLimit(ctx, req.Path); err != nil {
			return nil, err
		}
		resp, err := c.once(ctx, method, u, req)
		if err == nil && resp.StatusCode < 300 {
			return resp, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = &NetworkError{Method: method, URL: u.Redacted(), Err: err}
		} else {
			lastErr = newAPIError(method, u.Path, resp)
		}
		if attempt >= c.Retries || !shouldRetry(resp, err, retryable) {
			return resp, lastErr
		}
		wait := backoff(attempt)
		if resp != nil {
			if ra, ok := retryAfter(resp.Header.Get("Retry-After"), c.now()); ok {
				wait = ra
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				c.throttle(req.Path, wait)
			}
		}
		if c.Debug != nil {
			fmt.Fprintf(c.Debug, "* retrying in %s (attempt %d/%d): %v\n", wait.Round(time.Millisecond), attempt+1, c.Retries, lastErr)
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c *Client) once(ctx context.Context, method string, u *url.URL, req *Request) (*Response, error) {
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	hreq, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	for k, vs := range req.Header {
		for _, v := range vs {
			hreq.Header.Add(k, v)
		}
	}
	hreq.Header.Set("X-Api-Key", c.APIKey)
	if c.UserAgent != "" {
		hreq.Header.Set("User-Agent", c.UserAgent)
	}
	if hreq.Header.Get("Accept") == "" {
		hreq.Header.Set("Accept", "application/json")
	}
	// Mackerel API はボディのない DELETE でも Content-Type: application/json を要求する
	// （ないと 400 "Illegal Content-Type"）。GET/HEAD 以外には常に付ける
	if (req.Body != nil || (method != http.MethodGet && method != http.MethodHead)) && hreq.Header.Get("Content-Type") == "" {
		hreq.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	c.dumpRequest(hreq, req.Body)
	hresp, err := c.httpClient().Do(hreq)
	if err != nil {
		if c.Debug != nil {
			fmt.Fprintf(c.Debug, "* error after %s: %v\n", time.Since(start).Round(time.Millisecond), err)
		}
		return nil, err
	}
	defer hresp.Body.Close()
	b, err := io.ReadAll(hresp.Body)
	if err != nil {
		return nil, err
	}
	resp := &Response{StatusCode: hresp.StatusCode, Status: hresp.Status, Header: hresp.Header, Body: b}
	c.dumpResponse(resp, time.Since(start))
	return resp, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func shouldRetry(resp *Response, err error, retryable bool) bool {
	if !retryable {
		return false
	}
	if err != nil {
		var ne net.Error
		return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) || isConnError(err)
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func isConnError(err error) bool {
	var oe *net.OpError
	return errors.As(err, &oe)
}

const (
	backoffBase = 500 * time.Millisecond
	backoffMax  = 30 * time.Second
)

// backoff is exponential with full jitter in [d/2, d].
func backoff(attempt int) time.Duration {
	d := backoffBase << attempt
	if d <= 0 || d > backoffMax {
		d = backoffMax
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// retryAfter parses a Retry-After header (seconds or HTTP date).
func retryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return min(time.Duration(s)*time.Second, 5*time.Minute), true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return min(d, 5*time.Minute), true
	}
	return 0, false
}
