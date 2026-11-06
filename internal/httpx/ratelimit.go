package httpx

import (
	"context"
	"strings"
	"time"
)

// rateGroup maps a request path to a client-side rate limit group.
// The Mackerel API documents 1 request/second for Logs, Traces and APM.
func rateGroup(path string) (string, time.Duration) {
	p := strings.TrimPrefix(NormalizePath(path), APIPrefix)
	for _, prefix := range []string{"/logs", "/traces", "/apm/"} {
		if strings.HasPrefix(p, prefix) {
			return strings.Trim(prefix, "/"), time.Second
		}
	}
	return "", 0
}

// waitRateLimit blocks until the path's rate limit group allows another request.
func (c *Client) waitRateLimit(ctx context.Context, path string) error {
	group, interval := rateGroup(path)
	if group == "" {
		group = "default"
	}
	c.limiterMu.Lock()
	if c.nextSlot == nil {
		c.nextSlot = map[string]time.Time{}
	}
	now := c.now()
	slot := c.nextSlot[group]
	wait := slot.Sub(now)
	if wait < 0 {
		wait = 0
	}
	c.nextSlot[group] = now.Add(wait + interval)
	c.limiterMu.Unlock()
	if wait == 0 {
		return nil
	}
	return c.sleep(ctx, wait)
}

// throttle delays the next request of the path's group after a 429.
func (c *Client) throttle(path string, d time.Duration) {
	group, _ := rateGroup(path)
	if group == "" {
		group = "default"
	}
	c.limiterMu.Lock()
	defer c.limiterMu.Unlock()
	if c.nextSlot == nil {
		c.nextSlot = map[string]time.Time{}
	}
	if until := c.now().Add(d); until.After(c.nextSlot[group]) {
		c.nextSlot[group] = until
	}
}
