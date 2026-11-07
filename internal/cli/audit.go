package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/timeexpr"
)

// mcrl audit lists stale or forgotten settings (docs/design/decisions.md D22).
// It only reads.

const (
	severityInfo    = "info"
	severityWarning = "warning"
)

var severityRank = map[string]int{severityInfo: 1, severityWarning: 2}

type auditResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type auditFinding struct {
	Kind     string         `json:"kind"`
	Severity string         `json:"severity"`
	Resource auditResource  `json:"resource"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}

type auditCheck struct {
	kind     string
	severity string
	help     string
	run      func(a *auditor) ([]auditFinding, error)
}

// auditChecks is the list of checks, in output order.
var auditChecks = []auditCheck{
	{"muted-monitor", severityWarning, "monitors that are muted", (*auditor).mutedMonitors},
	{"ended-downtime", severityInfo, "downtimes that will never be in effect again", (*auditor).endedDowntimes},
	{"dangling-scope", severityWarning, "monitor and downtime scopes naming a service, role or monitor that does not exist", (*auditor).danglingScopes},
	{"host-inactive", severityInfo, "standby, maintenance or poweroff hosts with no metric for --older-than", (*auditor).inactiveHosts},
	{"host-stale-metrics", severityWarning, "working hosts whose agent metrics stopped more than --older-than ago", (*auditor).staleMetricHosts},
	{"host-without-role", severityInfo, "hosts with no role", (*auditor).hostsWithoutRole},
	{"alert-on-retired-host", severityWarning, "open alerts of retired hosts", (*auditor).alertsOnRetiredHosts},
	{"unused-service", severityInfo, "services with no host and no service metric", (*auditor).unusedServices},
	{"unused-role", severityInfo, "roles with no host", (*auditor).unusedRoles},
	{"unused-channel", severityInfo, "notification channels in no notification group", (*auditor).unusedChannels},
	{"empty-notification-group", severityWarning, "notification groups with no channel and no child group", (*auditor).emptyNotificationGroups},
	{"user-without-mfa", severityWarning, "users who have not enabled two-factor authentication", (*auditor).usersWithoutMFA},
	{"expired-invitation", severityInfo, "invitations past their expiry", (*auditor).expiredInvitations},
}

// auditProbeMetrics are the metrics read from GET /tsdb/latest to tell when
// a host last sent metrics. mackerel-agent sends them on Linux and Windows.
var auditProbeMetrics = []string{"loadavg5", "cpu.user.percentage", "memory.used"}

// auditLatestChunk is the number of hosts per GET /tsdb/latest request.
const auditLatestChunk = 100

// auditor fetches each list once and runs the checks on it.
type auditor struct {
	ctx       context.Context
	c         *mackerel.Client
	now       time.Time
	olderThan time.Duration

	lists   map[string][]json.RawMessage // path -> items
	latest  map[string]int64             // host ID -> newest probe metric time
	retired map[string]*auditHost        // host ID -> host fetched by ID (nil: gone)
}

func newAuditor(ctx context.Context, c *mackerel.Client, now time.Time, olderThan time.Duration) *auditor {
	return &auditor{ctx: ctx, c: c, now: now, olderThan: olderThan, lists: map[string][]json.RawMessage{}, retired: map[string]*auditHost{}}
}

// list returns the items of a list endpoint, fetching it only the first time.
func (a *auditor) list(path, key string) ([]json.RawMessage, error) {
	if items, ok := a.lists[path]; ok {
		return items, nil
	}
	var (
		items []json.RawMessage
		err   error
	)
	switch path {
	case "/hosts":
		items, err = a.c.List(a.ctx, path, key, url.Values{"status": mackerel.AllHostStatuses})
	case "/alerts":
		items, err = collect(a.c.NextIDPages(a.ctx, path, key, nil, mackerel.PageSize), 0, nil)
	default:
		items, err = a.c.List(a.ctx, path, key, nil)
	}
	if err != nil {
		return nil, err
	}
	a.lists[path] = items
	return items, nil
}

// decodeList fetches a list and decodes its items into T.
func decodeList[T any](a *auditor, path, key string) ([]T, error) {
	items, err := a.list(path, key)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		var v T
		if err := json.Unmarshal(it, &v); err != nil {
			return nil, fmt.Errorf("GET %s: %w", path, err)
		}
		out = append(out, v)
	}
	return out, nil
}

type auditHost struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Status    string              `json:"status"`
	CreatedAt int64               `json:"createdAt"`
	IsRetired bool                `json:"isRetired"`
	RetiredAt *int64              `json:"retiredAt"`
	Roles     map[string][]string `json:"roles"`
}

func (a *auditor) hosts() ([]auditHost, error) { return decodeList[auditHost](a, "/hosts", "hosts") }

type auditService struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

func (a *auditor) services() ([]auditService, error) {
	return decodeList[auditService](a, "/services", "services")
}

// latestMetrics returns the newest probe metric time of each host that has
// any of auditProbeMetrics.
func (a *auditor) latestMetrics() (map[string]int64, error) {
	if a.latest != nil {
		return a.latest, nil
	}
	hosts, err := a.hosts()
	if err != nil {
		return nil, err
	}
	latest := map[string]int64{}
	for chunk := range slices.Chunk(hosts, auditLatestChunk) {
		q := url.Values{"name": auditProbeMetrics}
		for _, h := range chunk {
			q.Add("hostId", h.ID)
		}
		b, err := a.c.Get(a.ctx, "/tsdb/latest", q)
		if err != nil {
			return nil, err
		}
		var resp struct {
			TSDBLatest map[string]map[string]*struct {
				Time int64 `json:"time"`
			} `json:"tsdbLatest"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, fmt.Errorf("GET /tsdb/latest: %w", err)
		}
		for id, metrics := range resp.TSDBLatest {
			for _, m := range metrics {
				if m != nil && m.Time > latest[id] {
					latest[id] = m.Time
				}
			}
		}
	}
	a.latest = latest
	return latest, nil
}

func (a *auditor) cutoff() time.Time { return a.now.Add(-a.olderThan) }

// olderThanCutoff reports whether epoch is at or before now - --older-than.
func (a *auditor) olderThanCutoff(epoch int64) bool {
	return !time.Unix(epoch, 0).After(a.cutoff())
}

func epochString(epoch int64) string { return time.Unix(epoch, 0).UTC().Format(time.RFC3339) }

// ---- checks ----

func (a *auditor) mutedMonitors() ([]auditFinding, error) {
	monitors, err := decodeList[struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		IsMute bool   `json:"isMute"`
	}](a, "/monitors", "monitors")
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, m := range monitors {
		if m.IsMute {
			out = append(out, auditFinding{
				Resource: auditResource{"monitor", m.ID, m.Name},
				Message:  "the monitor is muted and sends no notification",
				Details:  map[string]any{"type": m.Type},
			})
		}
	}
	return out, nil
}

type auditDowntime struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Start      int64  `json:"start"`
	Duration   int64  `json:"duration"`
	Recurrence *struct {
		Until *int64 `json:"until"`
	} `json:"recurrence"`
	ServiceScopes        []string `json:"serviceScopes"`
	ServiceExcludeScopes []string `json:"serviceExcludeScopes"`
	RoleScopes           []string `json:"roleScopes"`
	RoleExcludeScopes    []string `json:"roleExcludeScopes"`
	MonitorScopes        []string `json:"monitorScopes"`
	MonitorExcludeScopes []string `json:"monitorExcludeScopes"`
}

// downtimeEnd returns when a downtime stops for good. A recurring downtime
// without "until" never ends. For a recurring one, the end of the last
// occurrence is at most until + duration; that bound is used.
func downtimeEnd(d auditDowntime) (int64, bool) {
	length := d.Duration * 60
	if d.Recurrence == nil {
		return d.Start + length, true
	}
	if d.Recurrence.Until == nil {
		return 0, false
	}
	return *d.Recurrence.Until + length, true
}

func (a *auditor) downtimes() ([]auditDowntime, error) {
	return decodeList[auditDowntime](a, "/downtimes", "downtimes")
}

func (a *auditor) endedDowntimes() ([]auditFinding, error) {
	dts, err := a.downtimes()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, d := range dts {
		end, ok := downtimeEnd(d)
		if !ok || time.Unix(end, 0).After(a.now) {
			continue
		}
		out = append(out, auditFinding{
			Resource: auditResource{"downtime", d.ID, d.Name},
			Message:  "the downtime ended and will not be in effect again",
			Details:  map[string]any{"endedAt": end, "endedAtISO": epochString(end), "recurring": d.Recurrence != nil},
		})
	}
	return out, nil
}

// splitScope splits "service" or "service: role" (the form monitors and
// downtimes use) into its parts.
func splitScope(s string) (service, role string) {
	svc, r, ok := strings.Cut(s, ":")
	if !ok {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(svc), strings.TrimSpace(r)
}

func (a *auditor) danglingScopes() ([]auditFinding, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	roles := map[string]map[string]bool{}
	for _, s := range services {
		roles[s.Name] = map[string]bool{}
		for _, r := range s.Roles {
			roles[s.Name][r] = true
		}
	}
	monitors, err := decodeList[struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Type          string   `json:"type"`
		Service       string   `json:"service"`
		Scopes        []string `json:"scopes"`
		ExcludeScopes []string `json:"excludeScopes"`
	}](a, "/monitors", "monitors")
	if err != nil {
		return nil, err
	}
	monitorIDs := map[string]bool{}
	for _, m := range monitors {
		monitorIDs[m.ID] = true
	}
	// missing returns why a service or "service: role" scope does not resolve.
	missing := func(scope string) string {
		svc, role := splitScope(scope)
		rs, ok := roles[svc]
		switch {
		case !ok:
			return fmt.Sprintf("service %q does not exist", svc)
		case role != "" && !rs[role]:
			return fmt.Sprintf("role %q of service %q does not exist", role, svc)
		}
		return ""
	}
	var out []auditFinding
	add := func(res auditResource, field, scope, why string) {
		out = append(out, auditFinding{
			Resource: res,
			Message:  fmt.Sprintf("%s refers to something that does not exist: %s", field, why),
			Details:  map[string]any{"field": field, "scope": scope},
		})
	}
	for _, m := range monitors {
		res := auditResource{"monitor", m.ID, m.Name}
		if m.Type == "service" && m.Service != "" {
			if why := missing(m.Service); why != "" {
				add(res, "service", m.Service, why)
			}
		}
		for field, scopes := range map[string][]string{"scopes": m.Scopes, "excludeScopes": m.ExcludeScopes} {
			for _, s := range scopes {
				if why := missing(s); why != "" {
					add(res, field, s, why)
				}
			}
		}
	}
	dts, err := a.downtimes()
	if err != nil {
		return nil, err
	}
	for _, d := range dts {
		res := auditResource{"downtime", d.ID, d.Name}
		for field, scopes := range map[string][]string{
			"serviceScopes": d.ServiceScopes, "serviceExcludeScopes": d.ServiceExcludeScopes,
			"roleScopes": d.RoleScopes, "roleExcludeScopes": d.RoleExcludeScopes,
		} {
			for _, s := range scopes {
				if why := missing(s); why != "" {
					add(res, field, s, why)
				}
			}
		}
		for field, ids := range map[string][]string{"monitorScopes": d.MonitorScopes, "monitorExcludeScopes": d.MonitorExcludeScopes} {
			for _, id := range ids {
				if !monitorIDs[id] {
					add(res, field, id, fmt.Sprintf("monitor %q does not exist", id))
				}
			}
		}
	}
	// map の順序に依存しないように並べる
	slices.SortStableFunc(out, func(x, y auditFinding) int {
		return cmp.Or(cmp.Compare(x.Resource.ID, y.Resource.ID),
			cmp.Compare(x.Details["field"].(string), y.Details["field"].(string)),
			cmp.Compare(x.Details["scope"].(string), y.Details["scope"].(string)))
	})
	return out, nil
}

func (a *auditor) inactiveHosts() ([]auditFinding, error) {
	hosts, err := a.hosts()
	if err != nil {
		return nil, err
	}
	latest, err := a.latestMetrics()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, h := range hosts {
		if h.Status == "working" {
			continue
		}
		last, ok := latest[h.ID]
		details := map[string]any{"status": h.Status, "createdAt": h.CreatedAt}
		since := h.CreatedAt
		if ok {
			since = last
			details["lastMetricAt"] = last
		}
		if !a.olderThanCutoff(since) {
			continue
		}
		msg := fmt.Sprintf("the host is %s and has sent no metric since %s", h.Status, epochString(since))
		if !ok {
			msg = fmt.Sprintf("the host is %s and has sent no metric (registered %s)", h.Status, epochString(since))
		}
		out = append(out, auditFinding{Resource: auditResource{"host", h.ID, h.Name}, Message: msg, Details: details})
	}
	return out, nil
}

func (a *auditor) staleMetricHosts() ([]auditFinding, error) {
	hosts, err := a.hosts()
	if err != nil {
		return nil, err
	}
	latest, err := a.latestMetrics()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, h := range hosts {
		last, ok := latest[h.ID]
		if h.Status != "working" || !ok || !a.olderThanCutoff(last) {
			continue
		}
		out = append(out, auditFinding{
			Resource: auditResource{"host", h.ID, h.Name},
			Message:  fmt.Sprintf("the host is working but its agent metrics stopped at %s", epochString(last)),
			Details:  map[string]any{"status": h.Status, "lastMetricAt": last},
		})
	}
	return out, nil
}

func (a *auditor) hostsWithoutRole() ([]auditFinding, error) {
	hosts, err := a.hosts()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, h := range hosts {
		n := 0
		for _, rs := range h.Roles {
			n += len(rs)
		}
		if n == 0 {
			out = append(out, auditFinding{
				Resource: auditResource{"host", h.ID, h.Name},
				Message:  "the host has no role, so service and role scoped monitors do not cover it",
				Details:  map[string]any{"status": h.Status},
			})
		}
	}
	return out, nil
}

func (a *auditor) alertsOnRetiredHosts() ([]auditFinding, error) {
	alerts, err := decodeList[struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Type      string `json:"type"`
		MonitorID string `json:"monitorId"`
		HostID    string `json:"hostId"`
		OpenedAt  int64  `json:"openedAt"`
	}](a, "/alerts", "alerts")
	if err != nil {
		return nil, err
	}
	hosts, err := a.hosts()
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, h := range hosts {
		live[h.ID] = true
	}
	var out []auditFinding
	for _, al := range alerts {
		if al.HostID == "" || live[al.HostID] {
			continue
		}
		// 一覧にないホストは退役済みか削除済み。ホストごとに 1 回だけ取る
		h, seen := a.retired[al.HostID]
		if !seen {
			b, err := a.c.Get(a.ctx, "/hosts/"+url.PathEscape(al.HostID), nil)
			var nf *mackerel.NotFoundError
			switch {
			case err == nil:
				var v auditHost
				if json.Unmarshal(mackerel.UnwrapIf(b, "host"), &v) == nil {
					h = &v
				}
			case errors.As(mackerel.AsNotFound(err, "host", al.HostID), &nf):
				// 取れないホスト（削除済み）も退役済みとして扱う
			default:
				return nil, err
			}
			a.retired[al.HostID] = h
		}
		if h != nil && !h.IsRetired {
			continue
		}
		details := map[string]any{"alertStatus": al.Status, "monitorId": al.MonitorID, "hostId": al.HostID, "openedAt": al.OpenedAt}
		name := ""
		if h != nil {
			name = h.Name
			details["hostName"] = h.Name
			if h.RetiredAt != nil {
				details["retiredAt"] = *h.RetiredAt
			}
		}
		out = append(out, auditFinding{
			Resource: auditResource{"alert", al.ID, name},
			Message:  "the alert is still open but its host is retired; close it with `mcrl alert close`",
			Details:  details,
		})
	}
	return out, nil
}

// hostRoles counts the hosts of each service and "service:role".
func (a *auditor) hostRoles() (map[string]int, map[string]int, error) {
	hosts, err := a.hosts()
	if err != nil {
		return nil, nil, err
	}
	bySvc, byRole := map[string]int{}, map[string]int{}
	for _, h := range hosts {
		for svc, rs := range h.Roles {
			if len(rs) > 0 {
				bySvc[svc]++
			}
			for _, r := range rs {
				byRole[svc+":"+r]++
			}
		}
	}
	return bySvc, byRole, nil
}

func (a *auditor) unusedServices() ([]auditFinding, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	bySvc, _, err := a.hostRoles()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, s := range services {
		if bySvc[s.Name] > 0 {
			continue
		}
		// ホストのないサービスだけ、サービスメトリクスがあるかを確かめる
		names, err := a.c.List(a.ctx, "/services/"+url.PathEscape(s.Name)+"/metric-names", "names", nil)
		if err != nil {
			return nil, err
		}
		if len(names) > 0 {
			continue
		}
		out = append(out, auditFinding{
			Resource: auditResource{"service", s.Name, s.Name},
			Message:  "the service has no host and no service metric",
			Details:  map[string]any{"roles": len(s.Roles)},
		})
	}
	return out, nil
}

func (a *auditor) unusedRoles() ([]auditFinding, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	_, byRole, err := a.hostRoles()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, s := range services {
		for _, r := range s.Roles {
			full := s.Name + ":" + r
			if byRole[full] == 0 {
				out = append(out, auditFinding{
					Resource: auditResource{"role", full, full},
					Message:  "the role has no host",
				})
			}
		}
	}
	return out, nil
}

type auditNotificationGroup struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Type                      string   `json:"type"`
	ChildNotificationGroupIDs []string `json:"childNotificationGroupIds"`
	ChildChannelIDs           []string `json:"childChannelIds"`
}

func (a *auditor) notificationGroups() ([]auditNotificationGroup, error) {
	return decodeList[auditNotificationGroup](a, "/notification-groups", "notificationGroups")
}

func (a *auditor) unusedChannels() ([]auditFinding, error) {
	channels, err := decodeList[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}](a, "/channels", "channels")
	if err != nil {
		return nil, err
	}
	groups, err := a.notificationGroups()
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, g := range groups {
		for _, id := range g.ChildChannelIDs {
			used[id] = true
		}
	}
	var out []auditFinding
	for _, ch := range channels {
		if !used[ch.ID] {
			out = append(out, auditFinding{
				Resource: auditResource{"channel", ch.ID, ch.Name},
				Message:  "no notification group sends to the channel",
				Details:  map[string]any{"type": ch.Type},
			})
		}
	}
	return out, nil
}

func (a *auditor) emptyNotificationGroups() ([]auditFinding, error) {
	groups, err := a.notificationGroups()
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, g := range groups {
		if len(g.ChildChannelIDs) == 0 && len(g.ChildNotificationGroupIDs) == 0 {
			out = append(out, auditFinding{
				Resource: auditResource{"notification-group", g.ID, g.Name},
				Message:  "the notification group has no channel and no child group, so it notifies no one",
				Details:  map[string]any{"type": g.Type},
			})
		}
	}
	return out, nil
}

func (a *auditor) usersWithoutMFA() ([]auditFinding, error) {
	users, err := decodeList[struct {
		ID                    string   `json:"id"`
		ScreenName            string   `json:"screenName"`
		Email                 string   `json:"email"`
		Authority             string   `json:"authority"`
		IsMFAEnabled          bool     `json:"isMFAEnabled"`
		AuthenticationMethods []string `json:"authenticationMethods"`
	}](a, "/users", "users")
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, u := range users {
		if u.IsMFAEnabled {
			continue
		}
		out = append(out, auditFinding{
			Resource: auditResource{"user", u.ID, u.ScreenName},
			Message:  "the user has not enabled two-factor authentication",
			Details:  map[string]any{"email": u.Email, "authority": u.Authority, "authenticationMethods": u.AuthenticationMethods},
		})
	}
	return out, nil
}

func (a *auditor) expiredInvitations() ([]auditFinding, error) {
	invs, err := decodeList[struct {
		Email     string `json:"email"`
		Authority string `json:"authority"`
		ExpiresAt int64  `json:"expiresAt"`
	}](a, "/invitations", "invitations")
	if err != nil {
		return nil, err
	}
	var out []auditFinding
	for _, inv := range invs {
		if time.Unix(inv.ExpiresAt, 0).After(a.now) {
			continue
		}
		out = append(out, auditFinding{
			Resource: auditResource{"invitation", inv.Email, inv.Email},
			Message:  "the invitation has expired; revoke it with `mcrl invitation revoke`",
			Details:  map[string]any{"authority": inv.Authority, "expiresAt": inv.ExpiresAt},
		})
	}
	return out, nil
}

// run runs the checks in order and labels their findings.
func (a *auditor) run(checks []auditCheck) ([]auditFinding, error) {
	out := []auditFinding{}
	for _, ch := range checks {
		fs, err := ch.run(a)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ch.kind, err)
		}
		for _, f := range fs {
			f.Kind, f.Severity = ch.kind, ch.severity
			out = append(out, f)
		}
	}
	return out, nil
}

// ---- command ----

// ExitFindings is the exit code of "mcrl audit --fail-on" when it finds
// something at or above the level.
const ExitFindings = 8

func auditKinds() []string {
	kinds := make([]string, len(auditChecks))
	for i, c := range auditChecks {
		kinds[i] = c.kind
	}
	return kinds
}

func selectAuditChecks(kinds []string) ([]auditCheck, error) {
	if len(kinds) == 0 {
		return auditChecks, nil
	}
	want := map[string]bool{}
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if !contains(auditKinds(), k) {
			return nil, cmdutil.Usagef("unknown --check %q (valid: %s)", k, strings.Join(auditKinds(), ", "))
		}
		want[k] = true
	}
	var out []auditCheck
	for _, c := range auditChecks {
		if want[c.kind] {
			out = append(out, c)
		}
	}
	return out, nil
}

func newAuditCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		kinds          []string
		olderThan      string
		failOn, nowStr string
	)
	var list strings.Builder
	for _, c := range auditChecks {
		fmt.Fprintf(&list, "  %-26s %-8s %s\n", c.kind, c.severity, c.help)
	}
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "List forgotten or stale settings",
		Long: withAPI(`List settings that have likely been forgotten: muted monitors, ended
downtimes, hosts that stopped sending metrics, unused channels, users
without two-factor authentication, and so on. Nothing is changed.

Each finding is {"kind", "severity", "resource": {"type", "id", "name"},
"message", "details"}. Use --check to run some checks only, and --fail-on
to exit with code 8 when a finding at or above a severity is found (for CI
and cron).

Checks:
`+list.String()+`
Hosts are judged by the newest of `+strings.Join(auditProbeMetrics, ", ")+`
(GET /tsdb/latest). For hosts that have none of them, such as hosts
registered through the API only, host-stale-metrics does not apply and
host-inactive uses the registration time instead.`, "Read",
			"GET /api/v0/monitors", "GET /api/v0/downtimes", "GET /api/v0/services", "GET /api/v0/hosts",
			"GET /api/v0/tsdb/latest", "GET /api/v0/alerts", "GET /api/v0/hosts/<id>",
			"GET /api/v0/services/<name>/metric-names", "GET /api/v0/channels", "GET /api/v0/notification-groups",
			"GET /api/v0/users", "GET /api/v0/invitations"),
		Example: `  mcrl audit
  mcrl audit --check muted-monitor --check ended-downtime
  mcrl audit --older-than 7d -o jsonl --jq 'select(.severity == "warning") | .message'
  mcrl audit --fail-on warning > audit.json   # exit code 8 when a warning is found`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks, err := selectAuditChecks(kinds)
			if err != nil {
				return err
			}
			older, err := timeexpr.ParseDuration(olderThan)
			if err != nil || older <= 0 {
				return cmdutil.Usagef("invalid --older-than %q (e.g. 30d, 12h)", olderThan)
			}
			if failOn != "" && severityRank[failOn] == 0 {
				return cmdutil.Usagef("invalid --fail-on %q (valid: info, warning)", failOn)
			}
			now := time.Now()
			if nowStr != "" {
				if now, err = timeexpr.Parse(nowStr, now, time.Local); err != nil {
					return cmdutil.Usagef("invalid --now: %v", err)
				}
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			findings, err := newAuditor(cmd.Context(), c, now, older).run(checks)
			if err != nil {
				return err
			}
			items := make([]json.RawMessage, len(findings))
			for i, fd := range findings {
				if items[i], err = json.Marshal(fd); err != nil {
					return err
				}
			}
			if err := printItems(f, items); err != nil {
				return err
			}
			if failOn != "" {
				for _, fd := range findings {
					if severityRank[fd.Severity] >= severityRank[failOn] {
						return &cmdutil.ExitError{Code: ExitFindings, Silent: true}
					}
				}
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVar(&kinds, "check", nil, "run only this check (repeatable or comma-separated)")
	fl.StringVar(&olderThan, "older-than", "30d", "how long without metrics makes a host stale or inactive")
	fl.StringVar(&failOn, "fail-on", "", "exit with code 8 if a finding of this severity or higher is found: info or warning")
	fl.StringVar(&nowStr, "now", "", "judge as of this time instead of now")
	_ = fl.MarkHidden("now")
	_ = cmd.RegisterFlagCompletionFunc("check", cobra.FixedCompletions(auditKinds(), cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("fail-on", cobra.FixedCompletions([]string{severityInfo, severityWarning}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}
