package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"
)

// secretKeys are JSON object keys whose values are always hidden in debug output
// (compared case-insensitively). secretKey is the AWS integration's secret access key.
var secretKeys = map[string]bool{
	"secretkey":    true,
	"secret":       true,
	"password":     true,
	"token":        true,
	"accesstoken":  true,
	"refreshtoken": true,
	"apikey":       true,
	"clientsecret": true,
	"privatekey":   true,
}

// redactBody hides secrets in a JSON body for debug output. A body that is not
// JSON, or holds nothing to hide, is returned unchanged so that it reads exactly
// as sent. Otherwise it is re-encoded (object keys come out sorted).
//
// Besides the keys in secretKeys it hides:
//   - "url": everything after the host. Slack and webhook channel URLs are
//     credentials in themselves, and other URLs may carry tokens in the path or query.
//   - "headers" and "requestBody" of external monitors, which often hold the
//     credentials for the monitored endpoint.
func redactBody(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return body
	}
	if _, err := dec.Token(); err != io.EOF {
		return body // trailing data, e.g. JSON Lines
	}
	v, changed := redactValue("", v)
	if !changed {
		return body
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return body
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// redactValue redacts v, the value of key (empty for array elements and the root).
func redactValue(key string, v any) (any, bool) {
	k := strings.ToLower(key)
	switch {
	case secretKeys[k], k == "requestbody":
		if v == nil || v == "" {
			return v, false
		}
		return redacted, true
	case k == "headers":
		if v == nil {
			return v, false
		}
		if a, ok := v.([]any); ok && len(a) == 0 {
			return v, false
		}
		return redacted, true
	case k == "url":
		if s, ok := v.(string); ok {
			if r := redactURL(s); r != s {
				return r, true
			}
		}
		return v, false
	}
	changed := false
	switch t := v.(type) {
	case map[string]any:
		for mk, mv := range t {
			if nv, c := redactValue(mk, mv); c {
				t[mk] = nv
				changed = true
			}
		}
	case []any:
		for i, e := range t {
			if nv, c := redactValue("", e); c {
				t[i] = nv
				changed = true
			}
		}
	}
	return v, changed
}

// redactURL keeps the scheme and host of a URL and hides the rest.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		if s == "" {
			return s
		}
		return redacted
	}
	if u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" {
		return s
	}
	return u.Scheme + "://" + u.Host + "/" + redacted
}
