package mackerel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Kind describes how to list, get and name-match a resource type.
type Kind struct {
	Name     string // human name in messages, e.g. "monitor"
	ListPath string // e.g. "/monitors"
	ListKey  string // e.g. "monitors"
	// GetPath is the single-resource path with %s for the ID, or "" when the
	// API has no such endpoint and the list must be searched.
	GetPath string
	GetKey  string // wrapper key of the single-resource response, if any
	// NameFields are matched exactly against a non-ID reference.
	NameFields []string
}

// Kinds known to mcrl.
var (
	KindMonitor           = Kind{Name: "monitor", ListPath: "/monitors", ListKey: "monitors", GetPath: "/monitors/%s", GetKey: "monitor", NameFields: []string{"name"}}
	KindDashboard         = Kind{Name: "dashboard", ListPath: "/dashboards", ListKey: "dashboards", GetPath: "/dashboards/%s", NameFields: []string{"urlPath", "title"}}
	KindDowntime          = Kind{Name: "downtime", ListPath: "/downtimes", ListKey: "downtimes", NameFields: []string{"name"}}
	KindChannel           = Kind{Name: "channel", ListPath: "/channels", ListKey: "channels", NameFields: []string{"name"}}
	KindNotificationGroup = Kind{Name: "notification group", ListPath: "/notification-groups", ListKey: "notificationGroups", NameFields: []string{"name"}}
	KindAlertGroup        = Kind{Name: "alert group setting", ListPath: "/alert-group-settings", ListKey: "alertGroupSettings", GetPath: "/alert-group-settings/%s", GetKey: "alertGroupSetting", NameFields: []string{"name"}}
	KindUser              = Kind{Name: "user", ListPath: "/users", ListKey: "users", NameFields: []string{"email", "screenName"}}
	KindAWSIntegration    = Kind{Name: "AWS integration", ListPath: "/aws-integrations", ListKey: "aws_integrations", GetPath: "/aws-integrations/%s", NameFields: []string{"name"}}
	KindCheckMonitor      = Kind{Name: "check monitor", ListPath: "/monitoring/checks", ListKey: "checks", GetPath: "/monitoring/checks/%s", GetKey: "check", NameFields: []string{"name"}}
)

// IsID reports whether s looks like a Mackerel ID: 11 alphanumeric characters.
func IsID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !isAlnum {
			return false
		}
	}
	return true
}

type refMode int

const (
	refAuto refMode = iota
	refID
	refName
)

// parseRef handles the "id:" and "name:" prefixes that force how a
// reference is interpreted.
func parseRef(ref string) (string, refMode) {
	switch {
	case strings.HasPrefix(ref, "id:"):
		return ref[3:], refID
	case strings.HasPrefix(ref, "name:"):
		return ref[5:], refName
	}
	return ref, refAuto
}

// Resolve finds one resource by ID or name and returns its JSON.
func (c *Client) Resolve(ctx context.Context, k Kind, ref string) (json.RawMessage, error) {
	val, mode := parseRef(ref)
	if val == "" {
		return nil, fmt.Errorf("empty %s reference", k.Name)
	}
	if k.GetPath != "" && (mode == refID || mode == refAuto && IsID(val)) {
		body, err := c.Get(ctx, fmt.Sprintf(k.GetPath, url.PathEscape(val)), nil)
		if err == nil {
			return UnwrapIf(body, k.GetKey), nil
		}
		err = AsNotFound(err, k.Name, ref)
		var nf *NotFoundError
		if mode == refID || !errors.As(err, &nf) {
			return nil, err
		}
		// 名前がたまたま ID と同じ形をしている場合に備えて名前でも探す
	}
	items, err := c.List(ctx, k.ListPath, k.ListKey, nil)
	if err != nil {
		return nil, err
	}
	return match(k, ref, val, mode, items)
}

func match(k Kind, ref, val string, mode refMode, items []json.RawMessage) (json.RawMessage, error) {
	var hits []json.RawMessage
	var cands []Candidate
	for _, it := range items {
		obj := decodeObject(it)
		id, _ := obj["id"].(string)
		hit := mode != refName && id == val
		name := ""
		for _, f := range k.NameFields {
			s, _ := obj[f].(string)
			if name == "" {
				name = s
			}
			if mode != refID && s == val {
				hit = true
				name = s
			}
		}
		if hit {
			hits = append(hits, it)
			cands = append(cands, Candidate{ID: id, Name: name})
		}
	}
	switch len(hits) {
	case 0:
		return nil, &NotFoundError{Kind: k.Name, Ref: ref}
	case 1:
		return hits[0], nil
	}
	return nil, &AmbiguousError{Kind: k.Name, Ref: ref, Candidates: cands}
}

// AllHostStatuses makes GET /hosts return hosts in every status.
var AllHostStatuses = []string{"working", "standby", "maintenance", "poweroff"}

// ResolveHost finds a host by ID, exact name, or "cid:<customIdentifier>".
func (c *Client) ResolveHost(ctx context.Context, ref string) (json.RawMessage, error) {
	const kind = "host"
	if cid, ok := strings.CutPrefix(ref, "cid:"); ok {
		body, err := c.Get(ctx, "/hosts-by-custom-identifier/"+url.PathEscape(cid), nil)
		if err != nil {
			return nil, AsNotFound(err, kind, ref)
		}
		h, err := Field(body, "host")
		if err != nil {
			return nil, err
		}
		if IsNull(h) { // 見つからなくても 200 で {"host": null} が返る
			return nil, &NotFoundError{Kind: kind, Ref: ref}
		}
		return h, nil
	}
	val, mode := parseRef(ref)
	if val == "" {
		return nil, errors.New("empty host reference")
	}
	if mode == refID || mode == refAuto && IsID(val) {
		body, err := c.Get(ctx, "/hosts/"+url.PathEscape(val), nil)
		if err == nil {
			return Field(body, "host")
		}
		err = AsNotFound(err, kind, ref)
		var nf *NotFoundError
		if mode == refID || !errors.As(err, &nf) {
			return nil, err
		}
	}
	q := url.Values{"name": {val}, "status": AllHostStatuses}
	hosts, err := c.List(ctx, "/hosts", "hosts", q)
	if err != nil {
		return nil, err
	}
	switch len(hosts) {
	case 0:
		return nil, &NotFoundError{Kind: kind, Ref: ref}
	case 1:
		return hosts[0], nil
	}
	cands := make([]Candidate, len(hosts))
	for i, h := range hosts {
		cands[i] = Candidate{ID: StringField(h, "id"), Name: StringField(h, "name")}
	}
	return nil, &AmbiguousError{Kind: kind, Ref: ref, Candidates: cands}
}

// ResolveHostID is ResolveHost that returns only the ID, skipping the API
// call when ref is already an ID.
func (c *Client) ResolveHostID(ctx context.Context, ref string) (string, error) {
	if val, mode := parseRef(ref); mode == refID {
		return val, nil
	}
	h, err := c.ResolveHost(ctx, ref)
	if err != nil {
		return "", err
	}
	return StringField(h, "id"), nil
}

// ResolveID is Resolve that returns only the ID.
func (c *Client) ResolveID(ctx context.Context, k Kind, ref string) (string, error) {
	if val, mode := parseRef(ref); mode == refID {
		return val, nil
	}
	raw, err := c.Resolve(ctx, k, ref)
	if err != nil {
		return "", err
	}
	return StringField(raw, "id"), nil
}
