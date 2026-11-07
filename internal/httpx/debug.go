package httpx

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const redacted = "***"

func (c *Client) dumpRequest(req *http.Request, body []byte) {
	if c.Debug == nil {
		return
	}
	w := c.Debug
	fmt.Fprintf(w, "> %s %s\n", req.Method, req.URL.Redacted())
	dumpHeader(w, ">", req.Header)
	dumpBody(w, body)
}

func (c *Client) dumpResponse(resp *Response, elapsed time.Duration) {
	if c.Debug == nil {
		return
	}
	w := c.Debug
	fmt.Fprintf(w, "< %s (%s)\n", resp.Status, elapsed.Round(time.Millisecond))
	dumpHeader(w, "<", resp.Header)
	dumpBody(w, resp.Body)
}

func dumpHeader(w interface{ Write([]byte) (int, error) }, prefix string, h http.Header) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			if isSecretHeader(k) {
				v = redacted
			}
			fmt.Fprintf(w, "%s %s: %s\n", prefix, k, v)
		}
	}
}

func isSecretHeader(k string) bool {
	switch strings.ToLower(k) {
	case "x-api-key", "authorization", "cookie", "set-cookie":
		return true
	}
	return false
}

func dumpBody(w interface{ Write([]byte) (int, error) }, body []byte) {
	if len(body) == 0 {
		return
	}
	const maxDump = 64 << 10
	body = redactBody(body)
	b := body
	suffix := ""
	if len(b) > maxDump {
		b = b[:maxDump]
		suffix = fmt.Sprintf("\n... (%d bytes truncated)", len(body)-maxDump)
	}
	fmt.Fprintf(w, "%s%s\n", b, suffix)
}
