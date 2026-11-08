package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/timeexpr"
)

// mcrl triage gathers, in one read-only command, what is looked at in the
// first minutes of an incident (docs/usage.md): open alerts
// grouped by monitor, and around them the hosts, other failing monitors,
// metrics, graph annotations, downtimes, and optionally logs and traces.
// It reports facts and rule-based notes, never a guess at the cause.

// triageSources are the kinds of context --skip and --only choose from.
var triageSources = []string{"metrics", "monitors", "annotations", "downtimes", "logs", "traces"}

var triageStatuses = []string{"CRITICAL", "WARNING", "UNKNOWN"}

// triageStatusRank orders groups: the heaviest status first.
var triageStatusRank = map[string]int{"CRITICAL": 3, "WARNING": 2, "UNKNOWN": 1}

const (
	// triageConcurrency is how many requests run at once. Logs and traces
	// are further limited to 1 request/second by internal/httpx.
	triageConcurrency = 4

	// triageLastMetricLookback is how far before a connectivity alert opened
	// the time-series API is read, up to now, for the last metric of its
	// host, when GET /tsdb/latest has null values for the host.
	triageLastMetricLookback = 7 * 24 * time.Hour

	// triageMaxLogBody is the length (in bytes) a log body is cut to.
	triageMaxLogBody = 2000
	// triageMaxMetricMatches is how many metrics a wildcard (filesystem.*.used)
	// may expand to per host.
	triageMaxMetricMatches = 10
	// triageAlertLogLimit is how many status changes are read per alert
	// when alert IDs are given.
	triageAlertLogLimit = 50
)

// triageRateSources maps a rate ("%") monitor's metric to the metrics it is
// computed from. Reading the rate itself as a host metric returned 404 when
// tried; the alert's value has it.
var triageRateSources = map[string][]string{
	"cpu%":    {"cpu.idle.percentage"},
	"memory%": {"memory.used", "memory.total"},
	"swap%":   {"memory.swap_used", "memory.swap_total"},
	"disk%":   {"filesystem.*.used", "filesystem.*.size"},
}

// triageMetricFallback is tried when a metric does not exist (404).
// Some agents send memory.swap_free instead of memory.swap_used.
var triageMetricFallback = map[string]string{"memory.swap_used": "memory.swap_free"}

type triageOptions struct {
	ids                          []string
	service, role, host, monitor string
	statuses                     []string
	window                       time.Duration
	maxGroups, maxHosts          int
	maxSamples                   int
	sources                      map[string]bool
	otelServices                 []string
	now                          time.Time
}

// ---- output ----

type triageRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type triageResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type triageNote struct {
	Kind     string          `json:"kind"`
	Message  string          `json:"message"`
	Resource *triageResource `json:"resource,omitempty"`
}

type triageError struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type triageMetric struct {
	HostID  string            `json:"hostId,omitempty"`
	Service string            `json:"service,omitempty"`
	Name    string            `json:"name"`
	Points  []json.RawMessage `json:"points"`
}

type triageOtherMonitor struct {
	HostID    string `json:"hostId"`
	MonitorID string `json:"monitorId"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Status    string `json:"status"`
}

// triageContext is a group's surroundings. A source left out by --skip or
// --only is omitted, so an empty array always means "fetched, none".
type triageContext struct {
	Range         triageRange `json:"range"`
	Metrics       any         `json:"metrics,omitempty"`
	OtherMonitors any         `json:"otherMonitors,omitempty"`
	Annotations   any         `json:"annotations,omitempty"`
	Downtimes     any         `json:"downtimes,omitempty"`
}

type triageGroupOut struct {
	Status       string            `json:"status"`
	OpenedAt     int64             `json:"openedAt"`
	LastOpenedAt int64             `json:"lastOpenedAt"`
	Monitor      json.RawMessage   `json:"monitor"`
	Alerts       []json.RawMessage `json:"alerts"`
	Context      *triageContext    `json:"context,omitempty"`
	Notes        []triageNote      `json:"notes"`
	Next         []string          `json:"next"`
	Errors       []triageError     `json:"errors"`
}

type triageOtelOut struct {
	ServiceName string            `json:"serviceName"`
	Range       triageRange       `json:"range"`
	Logs        any               `json:"logs,omitempty"`
	Traces      any               `json:"traces,omitempty"`
	Next        []string          `json:"next"`
	Errors      []triageError     `json:"errors"`
	logs        []json.RawMessage // collected before Logs is set
	traces      []json.RawMessage
}

type triageSummary struct {
	Alerts   int `json:"alerts"`
	Open     int `json:"open"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Unknown  int `json:"unknown"`
	Groups   int `json:"groups"`
}

type triageOutput struct {
	GeneratedAt int64             `json:"generatedAt"`
	Summary     triageSummary     `json:"summary"`
	Groups      []*triageGroupOut `json:"groups"`
	Otel        []*triageOtelOut  `json:"otel"`
}

// ---- working state ----

type triageMonitor struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	IsMute  bool     `json:"isMute"`
	Metric  string   `json:"metric"`
	Service string   `json:"service"`
	Scopes  []string `json:"scopes"`
}

type triageAlert struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	MonitorID string `json:"monitorId"`
	HostID    string `json:"hostId"`
	Type      string `json:"type"`
	OpenedAt  int64  `json:"openedAt"`
	ClosedAt  *int64 `json:"closedAt"`
	// Value is passed through from the API as is. For an open alert it can
	// change over time, so it may be newer than the context, which stays
	// around the opening.
	Value json.RawMessage `json:"value"`

	raw  json.RawMessage
	host *auditHost // nil: no host, or not known (see hostGone)
	// hostGone is set when the host was looked up by ID and no longer exists.
	hostGone   bool
	hostErr    bool              // the lookup by ID failed
	probed     bool              // connectivity alerts: lastMetric was read
	lastMetric *int64            // the newest probe metric, if any
	logs       []json.RawMessage // alert IDs given: the status history
	// changedAt and changedTo are the time and the status of the latest entry
	// of the status history (detailed groups); nil when it could not be read.
	changedAt *int64
	changedTo string
}

type triageGroup struct {
	monitorID string
	monitor   triageMonitor
	monRaw    json.RawMessage
	alerts    []*triageAlert // newest first
	status    string
	first     int64 // earliest openedAt
	last      int64 // newest openedAt
	detailed  bool

	rng      triageRange
	hosts    []*triageAlert // one alert per host, up to --max-hosts
	services []string
	roles    []string

	metrics       []triageMetric
	otherMonitors []triageOtherMonitor
	annotations   []json.RawMessage
	downtimes     []json.RawMessage
	errors        []triageError
}

type triager struct {
	ctx  context.Context
	c    *mackerel.Client
	opts triageOptions

	mu       sync.Mutex // guards the groups' results while fetching
	monitors map[string]json.RawMessage
	hosts    map[string]*auditHost // from GET /hosts
}

// triageRunner runs fetches with at most triageConcurrency at a time.
type triageRunner struct {
	wg  sync.WaitGroup
	sem chan struct{}
}

func newTriageRunner() *triageRunner {
	return &triageRunner{sem: make(chan struct{}, triageConcurrency)}
}

func (r *triageRunner) Go(fn func()) {
	r.wg.Go(func() {
		r.sem <- struct{}{}
		defer func() { <-r.sem }()
		fn()
	})
}

func (r *triageRunner) Wait() { r.wg.Wait() }

func (t *triager) addError(g *triageGroup, source string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	g.errors = append(g.errors, triageError{Source: source, Message: err.Error()})
}

// ---- fetching ----

// fetchAlerts gets the given alerts, or every open alert.
func (t *triager) fetchAlerts() ([]json.RawMessage, error) {
	if len(t.opts.ids) == 0 {
		return collect(t.c.NextIDPages(t.ctx, "/alerts", "alerts", nil, mackerel.PageSize), 0, nil)
	}
	out := make([]json.RawMessage, len(t.opts.ids))
	errs := make([]error, len(t.opts.ids))
	r := newTriageRunner()
	for i, id := range t.opts.ids {
		r.Go(func() {
			b, err := t.c.Get(t.ctx, "/alerts/"+url.PathEscape(id), nil)
			if err != nil {
				errs[i] = notFoundByID(err, "alert", id)
				return
			}
			out[i] = mackerel.UnwrapIf(b, "alert")
		})
	}
	r.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

// fetchLists gets the monitors and, if any alert has a host, the hosts.
func (t *triager) fetchLists(needHosts bool) error {
	var monErr, hostErr error
	r := newTriageRunner()
	r.Go(func() { t.monitors, monErr = monitorsByID(t.ctx, t.c) })
	if needHosts {
		r.Go(func() {
			var items []json.RawMessage
			items, hostErr = t.c.List(t.ctx, "/hosts", "hosts", url.Values{"status": mackerel.AllHostStatuses})
			t.hosts = map[string]*auditHost{}
			for _, it := range items {
				var h auditHost
				if json.Unmarshal(it, &h) == nil {
					t.hosts[h.ID] = &h
				}
			}
		})
	}
	r.Wait()
	return errors.Join(monErr, hostErr)
}

func (t *triager) run() (*triageOutput, error) {
	raws, err := t.fetchAlerts()
	if err != nil {
		return nil, err
	}
	alerts := make([]*triageAlert, 0, len(raws))
	needHosts := false
	for _, raw := range raws {
		a := &triageAlert{raw: raw}
		if err := json.Unmarshal(raw, a); err != nil {
			return nil, fmt.Errorf("alert: %w", err)
		}
		needHosts = needHosts || a.HostID != ""
		alerts = append(alerts, a)
	}
	if err := t.fetchLists(needHosts); err != nil {
		return nil, err
	}
	for _, a := range alerts {
		if a.HostID != "" {
			a.host = t.hosts[a.HostID]
		}
	}
	alerts = slices.DeleteFunc(alerts, func(a *triageAlert) bool { return !t.keep(a) })
	groups := t.group(alerts)
	for i, g := range groups {
		g.detailed = i < t.opts.maxGroups
	}
	t.fetchRetiredHosts(groups)
	for _, g := range groups {
		t.prepare(g)
	}
	otel := t.fetchContext(groups)

	out := &triageOutput{GeneratedAt: t.opts.now.Unix(), Groups: []*triageGroupOut{}, Otel: otel}
	for _, a := range alerts {
		out.Summary.Alerts++
		if a.ClosedAt == nil {
			out.Summary.Open++
		}
		switch a.Status {
		case "CRITICAL":
			out.Summary.Critical++
		case "WARNING":
			out.Summary.Warning++
		case "UNKNOWN":
			out.Summary.Unknown++
		}
	}
	out.Summary.Groups = len(groups)
	for _, g := range groups {
		o, err := t.output(g)
		if err != nil {
			return nil, err
		}
		out.Groups = append(out.Groups, o)
	}
	return out, nil
}

// monitorOf returns the monitor of a group, decoded.
func (t *triager) monitorOf(id string) (triageMonitor, json.RawMessage) {
	raw, ok := t.monitors[id]
	if !ok {
		return triageMonitor{ID: id}, nil
	}
	var m triageMonitor
	_ = json.Unmarshal(raw, &m)
	return m, raw
}

// keep applies --status, --monitor, --host, --service and --role.
func (t *triager) keep(a *triageAlert) bool {
	o := t.opts
	if len(o.statuses) > 0 && !contains(o.statuses, a.Status) {
		return false
	}
	if o.monitor != "" && a.MonitorID != o.monitor {
		return false
	}
	if o.host != "" && a.HostID != o.host {
		return false
	}
	if o.service != "" || o.role != "" {
		m, _ := t.monitorOf(a.MonitorID)
		services, roles := hostRoleNames(a.host)
		if o.service != "" && m.Service != o.service && !contains(services, o.service) {
			return false
		}
		if o.role != "" && !contains(roles, o.role) {
			return false
		}
	}
	return true
}

// hostRoleNames returns the services and "service:role" names of a host.
func hostRoleNames(h *auditHost) (services, roles []string) {
	if h == nil {
		return nil, nil
	}
	for svc, rs := range h.Roles {
		services = append(services, svc)
		for _, r := range rs {
			roles = append(roles, svc+":"+r)
		}
	}
	slices.Sort(services)
	slices.Sort(roles)
	return services, roles
}

// group puts alerts into groups by monitor and ranks them: the heaviest
// status first, then the newest alert.
func (t *triager) group(alerts []*triageAlert) []*triageGroup {
	byMonitor := map[string]*triageGroup{}
	var groups []*triageGroup
	for _, a := range alerts {
		g, ok := byMonitor[a.MonitorID]
		if !ok {
			g = &triageGroup{monitorID: a.MonitorID}
			g.monitor, g.monRaw = t.monitorOf(a.MonitorID)
			if g.monitor.Type == "" {
				g.monitor.Type = a.Type
			}
			byMonitor[a.MonitorID] = g
			groups = append(groups, g)
		}
		g.alerts = append(g.alerts, a)
	}
	for _, g := range groups {
		slices.SortStableFunc(g.alerts, func(x, y *triageAlert) int {
			return cmp.Or(cmp.Compare(y.OpenedAt, x.OpenedAt), cmp.Compare(x.ID, y.ID))
		})
		g.first, g.last = g.alerts[0].OpenedAt, g.alerts[0].OpenedAt
		for _, a := range g.alerts {
			g.first, g.last = min(g.first, a.OpenedAt), max(g.last, a.OpenedAt)
			if triageStatusRank[a.Status] > triageStatusRank[g.status] || g.status == "" {
				g.status = a.Status
			}
		}
	}
	slices.SortStableFunc(groups, func(x, y *triageGroup) int {
		return cmp.Or(cmp.Compare(triageStatusRank[y.status], triageStatusRank[x.status]),
			cmp.Compare(y.last, x.last), cmp.Compare(x.monitorID, y.monitorID))
	})
	return groups
}

// fetchRetiredHosts looks up, by ID, the hosts of detailed groups that are
// not in the host list: retired (or deleted) hosts.
func (t *triager) fetchRetiredHosts(groups []*triageGroup) {
	type result struct {
		host *auditHost
		gone bool
		err  error
	}
	results := map[string]*result{}
	r := newTriageRunner()
	for _, g := range groups {
		if !g.detailed {
			continue
		}
		for _, a := range g.alerts {
			if a.HostID == "" || a.host != nil || results[a.HostID] != nil {
				continue
			}
			res := &result{}
			results[a.HostID] = res
			r.Go(func() {
				raw, err := hostByID(t.ctx, t.c, a.HostID)
				switch {
				case err != nil:
					res.err = err
				case raw == nil:
					res.gone = true
				default:
					var h auditHost
					if res.err = json.Unmarshal(raw, &h); res.err == nil {
						res.host = &h
					}
				}
			})
		}
	}
	r.Wait()
	for _, g := range groups {
		if !g.detailed {
			continue
		}
		reported := map[string]bool{}
		for _, a := range g.alerts {
			res := results[a.HostID]
			if res == nil {
				continue
			}
			a.host, a.hostGone, a.hostErr = res.host, res.gone, res.err != nil
			if res.err != nil && !reported[a.HostID] {
				reported[a.HostID] = true
				g.errors = append(g.errors, triageError{Source: "hosts", Message: fmt.Sprintf("host %s: %v", a.HostID, res.err)})
			}
		}
	}
}

// prepare works out a group's time range, hosts, services and roles.
func (t *triager) prepare(g *triageGroup) {
	from := time.Unix(g.first, 0).Add(-t.opts.window)
	to := time.Unix(g.first, 0).Add(t.opts.window)
	if to.After(t.opts.now) {
		to = t.opts.now
	}
	if to.Before(from) {
		to = from
	}
	g.rng = triageRange{From: from.Unix(), To: to.Unix()}
	seen := map[string]bool{}
	for _, a := range g.alerts {
		if a.HostID == "" || seen[a.HostID] || len(g.hosts) >= t.opts.maxHosts {
			continue
		}
		seen[a.HostID] = true
		g.hosts = append(g.hosts, a)
	}
	svcSet, roleSet := map[string]bool{}, map[string]bool{}
	if g.monitor.Service != "" {
		svcSet[g.monitor.Service] = true
	}
	for _, s := range g.monitor.Scopes {
		svc, role := splitScope(s)
		svcSet[svc] = true
		if role != "" {
			roleSet[svc+":"+role] = true
		}
	}
	for _, a := range g.alerts {
		services, roles := hostRoleNames(a.host)
		for _, s := range services {
			svcSet[s] = true
		}
		for _, r := range roles {
			roleSet[r] = true
		}
	}
	g.services = uniqueSorted(slices.Collect(mapKeys(svcSet)))
	g.roles = uniqueSorted(slices.Collect(mapKeys(roleSet)))
}

func mapKeys(m map[string]bool) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// fetchContext fetches everything around the detailed groups, and the logs
// and traces of --otel-service, concurrently.
func (t *triager) fetchContext(groups []*triageGroup) []*triageOtelOut {
	var detailed []*triageGroup
	for _, g := range groups {
		if g.detailed {
			detailed = append(detailed, g)
		}
	}
	src := t.opts.sources
	r := newTriageRunner()

	// 疎通監視のアラートのホストは、最後にメトリクスを送った時刻を 1 回でまとめて取る
	var probeIDs []string
	for _, g := range detailed {
		for _, a := range g.alerts {
			if a.Type == "connectivity" && a.HostID != "" {
				if !contains(probeIDs, a.HostID) {
					probeIDs = append(probeIDs, a.HostID)
				}
			}
		}
	}
	if len(probeIDs) > 0 {
		r.Go(func() {
			// 範囲は、そのホストの最も早い疎通監視のアラートが開く 7 日前から今まで。
			// アラートのあとに送られたメトリクスも拾い、注記は最後の点がアラートより前のときだけ出す
			opened := map[string]int64{}
			for _, g := range detailed {
				for _, a := range g.alerts {
					if a.Type == "connectivity" {
						if o, ok := opened[a.HostID]; !ok || a.OpenedAt < o {
							opened[a.HostID] = a.OpenedAt
						}
					}
				}
			}
			now := t.opts.now.Unix()
			latest, _, failed, err := latestMetricTimes(t.ctx, t.c, probeIDs, func(id string) (int64, int64) {
				return opened[id] - int64(triageLastMetricLookback/time.Second), now
			}, 1) // this already holds one slot of r: look up one host at a time
			for _, g := range detailed {
				reported := map[string]bool{}
				for _, a := range g.alerts {
					if a.Type != "connectivity" || a.HostID == "" {
						continue
					}
					if err != nil {
						t.addError(g, "hosts", fmt.Errorf("last metric time: %w", err))
						break
					}
					if herr := failed[a.HostID]; herr != nil {
						// nothing is said about the host; the others keep their time
						if !reported[a.HostID] {
							reported[a.HostID] = true
							t.addError(g, "hosts", fmt.Errorf("last metric time of host %s: %w", a.HostID, herr))
						}
						continue
					}
					a.probed = true
					if v, ok := latest[a.HostID]; ok {
						a.lastMetric = &v
					}
				}
			}
		})
	}

	var downtimes []json.RawMessage
	var downtimeErr error
	if src["downtimes"] && len(detailed) > 0 {
		r.Go(func() { downtimes, downtimeErr = t.c.List(t.ctx, "/downtimes", "downtimes", nil) })
	}

	for _, g := range detailed {
		// 状態の履歴は、アラートの value がいつの値かを知るために読む。
		// メトリクスと同じ、--max-hosts 件のアラートだけ（ホストのないアラートは新しい順に --max-hosts 件）。
		// アラート ID を指定したときは全アラートを読み、履歴そのものも出力する
		for _, a := range t.historyAlerts(g) {
			r.Go(func() {
				logs, err := collect(t.c.NextIDPages(t.ctx, "/alerts/"+url.PathEscape(a.ID)+"/logs", "logs", nil, mackerel.PageSize), triageAlertLogLimit, nil)
				if err != nil {
					t.addError(g, "alertLogs", fmt.Errorf("alert %s: %w", a.ID, err))
					return
				}
				a.changedAt, a.changedTo = latestStatusChange(logs)
				if len(t.opts.ids) > 0 {
					a.logs = logs
				}
			})
		}
		if src["metrics"] {
			t.fetchMetrics(r, g)
		}
		if src["monitors"] {
			for _, a := range g.hosts {
				if a.host == nil || a.host.IsRetired {
					continue
				}
				r.Go(func() { t.fetchOtherMonitors(g, a.HostID) })
			}
		}
		if src["annotations"] {
			for _, svc := range g.services {
				r.Go(func() { t.fetchAnnotations(g, svc) })
			}
		}
	}

	otel := t.otelOutputs(detailed)
	for _, o := range otel {
		if src["logs"] {
			r.Go(func() { t.fetchLogs(o) })
		}
		if src["traces"] {
			r.Go(func() { t.fetchTraces(o) })
		}
	}
	r.Wait()

	for _, g := range detailed {
		if !src["downtimes"] {
			continue
		}
		if downtimeErr != nil {
			g.errors = append(g.errors, triageError{Source: "downtimes", Message: downtimeErr.Error()})
			continue
		}
		g.downtimes = []json.RawMessage{}
		for _, raw := range downtimes {
			var d auditDowntime
			if json.Unmarshal(raw, &d) != nil || !downtimeActive(raw, t.opts.now) {
				continue
			}
			if downtimeCovers(d, g.monitorID, g.services, g.roles) {
				g.downtimes = append(g.downtimes, raw)
			}
		}
	}
	for _, o := range otel {
		slices.SortStableFunc(o.Errors, func(x, y triageError) int { return cmp.Compare(x.Source, y.Source) })
		if src["logs"] && o.logs != nil {
			o.Logs = o.logs
		}
		if src["traces"] && o.traces != nil {
			o.Traces = o.traces
		}
	}
	return otel
}

// historyAlerts returns the alerts whose status history is read: all of them
// when alert IDs are given, otherwise those whose hosts get context, and the
// newest --max-hosts alerts that have no host.
func (t *triager) historyAlerts(g *triageGroup) []*triageAlert {
	if len(t.opts.ids) > 0 {
		return g.alerts
	}
	out := slices.Clone(g.hosts)
	hostless := 0
	for _, a := range g.alerts {
		if a.HostID == "" && hostless < t.opts.maxHosts {
			out = append(out, a)
			hostless++
		}
	}
	return out
}

// latestStatusChange returns the time and the status of the newest entry of
// an alert's status history, or nil when there is none.
func latestStatusChange(logs []json.RawMessage) (*int64, string) {
	var at *int64
	var status string
	for _, raw := range logs {
		var l struct {
			Status    string `json:"status"`
			CreatedAt *int64 `json:"createdAt"`
		}
		if json.Unmarshal(raw, &l) != nil || l.CreatedAt == nil {
			continue
		}
		if at == nil || *l.CreatedAt > *at {
			at, status = l.CreatedAt, l.Status
		}
	}
	return at, status
}

// downtimeCovers reports whether a downtime's scopes cover a monitor, given
// the services and "service:role" names of what it watches. A downtime
// with no scope covers every monitor; exclusions win over inclusions.
func downtimeCovers(d auditDowntime, monitorID string, services, roles []string) bool {
	norm := func(scopes []string) []string {
		out := make([]string, len(scopes))
		for i, s := range scopes {
			svc, role := splitScope(s)
			out[i] = svc
			if role != "" {
				out[i] = svc + ":" + role
			}
		}
		return out
	}
	anyIn := func(list, scopes []string) bool {
		return slices.ContainsFunc(list, func(s string) bool { return contains(scopes, s) })
	}
	if contains(d.MonitorExcludeScopes, monitorID) || anyIn(services, d.ServiceExcludeScopes) || anyIn(roles, norm(d.RoleExcludeScopes)) {
		return false
	}
	if len(d.MonitorScopes)+len(d.ServiceScopes)+len(d.RoleScopes) == 0 {
		return true
	}
	return contains(d.MonitorScopes, monitorID) || anyIn(services, d.ServiceScopes) || anyIn(roles, norm(d.RoleScopes))
}

// triageMetricPlan returns the metrics to fetch for a monitor's metric, or
// why there are none.
func triageMetricPlan(metric string) ([]string, error) {
	if src, ok := triageRateSources[metric]; ok {
		return src, nil
	}
	if strings.HasSuffix(metric, "%") {
		return nil, fmt.Errorf("%s is a rate monitor whose source metrics are unknown, so no metric is shown; the alert's value has the rate", metric)
	}
	return []string{metric}, nil
}

// metricPattern turns "filesystem.*.used" into a regexp where * matches
// one dot-separated part.
func metricPattern(name string) *regexp.Regexp {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(name), `\*`, `[^.]+`) + "$")
}

// fetchMetrics queues the metric fetches of a group: the monitored metric
// of each host (host monitors) or of the service (service monitors).
func (t *triager) fetchMetrics(r *triageRunner, g *triageGroup) {
	m := g.monitor
	if m.Metric == "" || (m.Type != "host" && m.Type != "service") {
		return
	}
	names, err := triageMetricPlan(m.Metric)
	if err != nil {
		t.addError(g, "metrics", err) // 他の取得がすでに走っているので、ロックして足す
		return
	}
	if m.Type == "service" {
		if m.Service == "" {
			return
		}
		r.Go(func() {
			t.fetchMetricSet(g, "/services/"+url.PathEscape(m.Service), "service "+m.Service, names,
				func(name string, pts []json.RawMessage) triageMetric {
					return triageMetric{Service: m.Service, Name: name, Points: pts}
				})
		})
		return
	}
	for _, a := range g.hosts {
		if a.hostGone {
			continue
		}
		r.Go(func() {
			t.fetchMetricSet(g, "/hosts/"+url.PathEscape(a.HostID), "host "+a.HostID, names,
				func(name string, pts []json.RawMessage) triageMetric {
					return triageMetric{HostID: a.HostID, Name: name, Points: pts}
				})
		})
	}
}

// fetchMetricSet fetches metrics of one host or service in the group's
// range. Names with * are matched against the metric names first.
func (t *triager) fetchMetricSet(g *triageGroup, base, label string, names []string, mk func(string, []json.RawMessage) triageMetric) {
	var resolved []string
	var known []json.RawMessage
	for _, n := range names {
		if !strings.Contains(n, "*") {
			resolved = append(resolved, n)
			continue
		}
		if known == nil {
			var err error
			if known, err = t.c.List(t.ctx, base+"/metric-names", "names", nil); err != nil {
				t.addError(g, "metrics", fmt.Errorf("%s: metric names: %w", label, err))
				return
			}
		}
		re := metricPattern(n)
		var hits []string
		for _, raw := range known {
			var s string
			if json.Unmarshal(raw, &s) == nil && re.MatchString(s) {
				hits = append(hits, s)
			}
		}
		slices.Sort(hits)
		if len(hits) > triageMaxMetricMatches {
			t.addError(g, "metrics", fmt.Errorf("%s: %d metrics match %s; only the first %d are shown", label, len(hits), n, triageMaxMetricMatches))
			hits = hits[:triageMaxMetricMatches]
		}
		resolved = append(resolved, hits...)
	}
	q := url.Values{"from": {strconv.FormatInt(g.rng.From, 10)}, "to": {strconv.FormatInt(g.rng.To, 10)}}
	for _, n := range resolved {
		q.Set("name", n)
		pts, err := t.c.List(t.ctx, base+"/metrics", "metrics", q)
		if alt, ok := triageMetricFallback[n]; ok && isNotFound(err) {
			n = alt
			q.Set("name", n)
			pts, err = t.c.List(t.ctx, base+"/metrics", "metrics", q)
		}
		if err != nil {
			t.addError(g, "metrics", fmt.Errorf("%s: %s: %w", label, n, err))
			continue
		}
		t.mu.Lock()
		g.metrics = append(g.metrics, mk(n, pts))
		t.mu.Unlock()
	}
}

// isNotFound reports whether err is a 404 from the API.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf *mackerel.NotFoundError
	return errors.As(mackerel.AsNotFound(err, "", ""), &nf)
}

// fetchOtherMonitors adds the monitors other than the group's that are not
// OK on a host.
func (t *triager) fetchOtherMonitors(g *triageGroup, hostID string) {
	items, err := t.c.List(t.ctx, "/hosts/"+url.PathEscape(hostID)+"/monitored-statuses", "monitoredStatuses", nil)
	if err != nil {
		t.addError(g, "monitors", fmt.Errorf("host %s: %w", hostID, err))
		return
	}
	var found []triageOtherMonitor
	for _, it := range items {
		id, status := mackerel.StringField(it, "monitorId"), mackerel.StringField(it, "status")
		if status == "OK" || id == g.monitorID {
			continue
		}
		m, _ := t.monitorOf(id)
		found = append(found, triageOtherMonitor{HostID: hostID, MonitorID: id, Name: m.Name, Type: m.Type, Status: status})
	}
	t.mu.Lock()
	g.otherMonitors = append(g.otherMonitors, found...)
	t.mu.Unlock()
}

func (t *triager) fetchAnnotations(g *triageGroup, service string) {
	q := url.Values{"service": {service}, "from": {strconv.FormatInt(g.rng.From, 10)}, "to": {strconv.FormatInt(g.rng.To, 10)}}
	items, err := t.c.List(t.ctx, "/graph-annotations", "graphAnnotations", q)
	if err != nil {
		t.addError(g, "annotations", fmt.Errorf("service %s: %w", service, err))
		return
	}
	t.mu.Lock()
	g.annotations = append(g.annotations, items...)
	t.mu.Unlock()
}

// otelOutputs prepares an entry per --otel-service. The range is around
// the earliest alert of the detailed groups (or now, with no alert).
func (t *triager) otelOutputs(detailed []*triageGroup) []*triageOtelOut {
	out := []*triageOtelOut{}
	if len(t.opts.otelServices) == 0 {
		return out
	}
	start := t.opts.now
	for _, g := range detailed {
		if s := time.Unix(g.first, 0); s.Before(start) {
			start = s
		}
	}
	to := start.Add(t.opts.window)
	if to.After(t.opts.now) {
		to = t.opts.now
	}
	rng := triageRange{From: start.Add(-t.opts.window).Unix(), To: to.Unix()}
	for _, svc := range t.opts.otelServices {
		o := &triageOtelOut{ServiceName: svc, Range: rng, Next: []string{}, Errors: []triageError{}}
		span := fmt.Sprintf("--since @%d --until @%d", rng.From, rng.To)
		if t.opts.sources["logs"] {
			o.Next = append(o.Next, fmt.Sprintf("mcrl log search --service %s %s --severity error+", shellQuote(svc), span))
		}
		if t.opts.sources["traces"] {
			o.Next = append(o.Next, fmt.Sprintf("mcrl trace list --service %s %s --status error --sort latency", shellQuote(svc), span))
		}
		out = append(out, o)
	}
	return out
}

// fetchLogs reads error logs the way "log search --severity error+" does.
func (t *triager) fetchLogs(o *triageOtelOut) {
	l := logFlags{service: o.ServiceName, severities: []string{"error+"}, limit: t.opts.maxSamples}
	changed := func(name string) bool { return name == "service" || name == "severity" }
	body, err := l.body(t.ctx, changed, t.c)
	if err == nil {
		l.setRange(body, o.Range.From, o.Range.To)
		var items []json.RawMessage
		if items, err = collect(t.c.CursorPages(t.ctx, "/logs", body), t.opts.maxSamples, nil); err == nil {
			for i, it := range items {
				if items[i], err = truncateLogBody(it); err != nil {
					break
				}
			}
			o.logs = items
		}
	}
	if err != nil {
		t.mu.Lock()
		o.Errors = append(o.Errors, triageError{Source: "logs", Message: err.Error()})
		t.mu.Unlock()
	}
}

// truncateLogBody cuts a long "body" and adds "truncated".
func truncateLogBody(raw json.RawMessage) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	truncated := false
	var body string
	if json.Unmarshal(m["body"], &body) == nil && len(body) > triageMaxLogBody {
		cut := triageMaxLogBody
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}
		b, err := json.Marshal(body[:cut])
		if err != nil {
			return nil, err
		}
		m["body"], truncated = b, true
	}
	m["truncated"] = json.RawMessage(strconv.FormatBool(truncated))
	return json.Marshal(m)
}

// fetchTraces reads error traces the way "trace list --status error --sort
// latency" does.
func (t *triager) fetchTraces(o *triageOtelOut) {
	tf := traceFlags{service: o.ServiceName, status: "error", sort: "latency", limit: t.opts.maxSamples}
	body, err := tf.body(func(name string) bool { return name == "status" }, o.Range.From, o.Range.To)
	if err == nil {
		o.traces, err = collect(tracePages(t.ctx, t.c, body), t.opts.maxSamples, nil)
	}
	if err != nil {
		t.mu.Lock()
		o.Errors = append(o.Errors, triageError{Source: "traces", Message: err.Error()})
		t.mu.Unlock()
	}
}

// ---- notes, next commands and output ----

// humanDuration writes a duration as "45s", "3m" or "2h5m".
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// beforeAfter says how far epoch is from ref, e.g. "3m before".
func beforeAfter(epoch, ref int64) string {
	d := time.Duration(epoch-ref) * time.Second
	if d < 0 {
		return humanDuration(d) + " before"
	}
	return humanDuration(d) + " after"
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9@%+=:,./_-]+$`)

// shellQuote quotes s for a POSIX shell when needed.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// statusChangedOutsideRange reports whether the alert changed status after
// it opened, and that change is outside the
// context range.
func (a *triageAlert) statusChangedOutsideRange(rng triageRange) bool {
	return a.changedAt != nil && *a.changedAt != a.OpenedAt && (*a.changedAt < rng.From || *a.changedAt > rng.To)
}

func hostLabel(a *triageAlert) string {
	if a.host != nil && a.host.Name != "" {
		return a.host.Name
	}
	return a.HostID
}

func (t *triager) notes(g *triageGroup) []triageNote {
	notes := []triageNote{}
	add := func(kind, msg string, res *triageResource) {
		notes = append(notes, triageNote{Kind: kind, Message: msg, Resource: res})
	}
	if g.monitor.IsMute {
		add("monitor-muted", "the monitor is muted, so these alerts send no notification",
			&triageResource{"monitor", g.monitorID, g.monitor.Name})
	}
	for _, raw := range g.downtimes {
		id, name := mackerel.StringField(raw, "id"), mackerel.StringField(raw, "name")
		// ダウンタイム中は新しいアラートが作られない（api-notes.md）。今有効なら、アラートの後に始まった可能性が高い
		add("in-downtime", fmt.Sprintf("the downtime %q is in effect now and covers this monitor; downtimes stop new alerts, so it most likely started after the alert opened (someone may be working on it)", name),
			&triageResource{"downtime", id, name})
	}
	hosts := map[string]bool{}
	for _, a := range g.alerts {
		if a.HostID == "" || hosts[a.HostID] {
			continue
		}
		hosts[a.HostID] = true
		res := &triageResource{"host", a.HostID, hostLabel(a)}
		switch {
		case a.hostGone:
			add("host-retired", fmt.Sprintf("the host %s no longer exists, but alert %s is still open", a.HostID, a.ID), res)
		case a.host != nil && hostRetired(a.host):
			add("host-retired", fmt.Sprintf("the host %q is retired, but alert %s is still open", a.host.Name, a.ID), res)
		case a.host != nil && a.host.Status != "" && a.host.Status != "working":
			add("host-not-working", fmt.Sprintf("the host %q is %s", a.host.Name, a.host.Status), res)
		}
		if a.lastMetric != nil && *a.lastMetric < a.OpenedAt {
			add("metrics-stopped", fmt.Sprintf("the host %q last sent metrics at %s, %s the alert opened",
				hostLabel(a), epochString(*a.lastMetric), beforeAfter(*a.lastMetric, a.OpenedAt)), res)
		}
	}
	for _, a := range g.alerts {
		if !a.statusChangedOutsideRange(g.rng) {
			continue
		}
		msg := fmt.Sprintf("the alert %q changed to %s at %s, %s after it opened, outside the context range", a.ID, a.changedTo, epochString(*a.changedAt), humanDuration(time.Duration(*a.changedAt-a.OpenedAt)*time.Second))
		add("status-changed", msg, &triageResource{"alert", a.ID, ""})
	}
	for _, raw := range g.annotations {
		var an struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			From    int64  `json:"from"`
			Service string `json:"service"`
		}
		if json.Unmarshal(raw, &an) != nil {
			continue
		}
		add("recent-annotation", fmt.Sprintf("an annotation %q on service %q started %s the alert", an.Title, an.Service, beforeAfter(an.From, g.first)),
			&triageResource{"annotation", an.ID, an.Title})
	}
	if len(hosts) >= 2 {
		add("many-hosts", fmt.Sprintf("%d hosts are alerting on this monitor", len(hosts)), nil)
	}
	byHost := map[string][]string{}
	var order []string
	for _, om := range g.otherMonitors {
		if _, ok := byHost[om.HostID]; !ok {
			order = append(order, om.HostID)
		}
		byHost[om.HostID] = append(byHost[om.HostID], fmt.Sprintf("%s (%s)", cmp.Or(om.Name, om.MonitorID), om.Status))
	}
	for _, hid := range order {
		name := hid
		for _, a := range g.alerts {
			if a.HostID == hid {
				name = hostLabel(a)
				break
			}
		}
		add("other-monitors-failing", fmt.Sprintf("other monitors are not OK on the host %q: %s", name, strings.Join(byHost[hid], ", ")),
			&triageResource{"host", hid, name})
	}
	return notes
}

// metricGetCommand is the "metric get" command that reads a shown metric
// over the given --since (and --until) flags.
func metricGetCommand(m triageMetric, span string) string {
	target := "--host " + shellQuote("id:"+m.HostID)
	if m.Service != "" {
		target = "--service " + shellQuote(m.Service)
	}
	return fmt.Sprintf("mcrl metric get %s --name %s %s", target, shellQuote(m.Name), span)
}

func (t *triager) next(g *triageGroup) []string {
	next := []string{}
	if !g.detailed {
		if g.monitorID == "" {
			return append(next, "mcrl alert get "+g.alerts[0].ID+" --expand")
		}
		return append(next, "mcrl triage --monitor "+shellQuote("id:"+g.monitorID))
	}
	if len(t.opts.ids) == 0 {
		next = append(next, "mcrl alert logs "+g.alerts[0].ID)
	}
	since := fmt.Sprintf("--since @%d", g.rng.From-3*3600)
	seen := map[string]bool{}
	for _, m := range g.metrics {
		if len(seen) >= 3 {
			break
		}
		key := m.HostID + m.Service + "\x00" + m.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		next = append(next, metricGetCommand(m, since))
	}
	// 状態が変わった時刻の前後は context の範囲外なので、別に読む
	var recent []string
	recentDone := false
	for _, a := range g.alerts {
		if !a.statusChangedOutsideRange(g.rng) {
			continue
		}
		at := time.Unix(*a.changedAt, 0)
		until := at.Add(t.opts.window)
		if until.After(t.opts.now) {
			until = t.opts.now
		}
		span := fmt.Sprintf("--since @%d --until @%d", at.Add(-t.opts.window).Unix(), until.Unix())
		n := 0
		for _, m := range g.metrics {
			if n >= 3 {
				break
			}
			// ホストのメトリクスは、そのアラートのホストのものだけ
			if (m.Service == "" && m.HostID == a.HostID) || (m.Service != "" && a.HostID == "") {
				next = append(next, metricGetCommand(m, span))
				if !recentDone {
					// value は今に近いかもしれないので、直近 --window も 1 グループにつき最初のアラート分だけ読む
					recent = append(recent, metricGetCommand(m, "--since "+humanDuration(t.opts.window)))
				}
				n++
			}
		}
		recentDone = true
	}
	next = append(next, recent...)
	for _, a := range g.hosts {
		if a.host != nil && !a.host.IsRetired {
			next = append(next, "mcrl host monitor-status "+shellQuote("id:"+a.HostID)+" --expand")
			break
		}
	}
	for i, svc := range g.services {
		if i >= 2 {
			break
		}
		next = append(next, fmt.Sprintf("mcrl annotation list --service %s --since @%d", shellQuote(svc), g.rng.From-24*3600))
	}
	for _, a := range g.alerts {
		if a.ClosedAt == nil && (a.hostGone || (a.host != nil && a.host.IsRetired)) {
			next = append(next, fmt.Sprintf("mcrl alert close %s --reason 'host retired'", a.ID))
		}
	}
	return next
}

// alertJSON is the API's alert with "host", "statusChangedAt" and, when alert
// IDs are given, "logs" added.
func (t *triager) alertJSON(a *triageAlert) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(a.raw, &m); err != nil {
		return nil, err
	}
	if a.HostID != "" {
		h := map[string]any{"id": a.HostID}
		if a.host != nil {
			_, roles := hostRoleNames(a.host)
			h["name"], h["status"], h["isRetired"], h["roles"] = a.host.Name, a.host.Status, a.host.IsRetired, orEmpty(roles)
			if a.host.RetiredAt != nil {
				h["retiredAt"] = *a.host.RetiredAt
			}
			if n := len(a.host.Meta.CPU); n > 0 {
				h["cpuCount"] = n
			}
		}
		if a.hostGone {
			h["isRetired"] = true
		}
		if a.probed {
			h["lastMetricAt"] = a.lastMetric
		}
		b, err := json.Marshal(h)
		if err != nil {
			return nil, err
		}
		m["host"] = b
	}
	if a.changedAt != nil {
		b, err := json.Marshal(*a.changedAt)
		if err != nil {
			return nil, err
		}
		m["statusChangedAt"] = b
	}
	if a.logs != nil {
		b, err := json.Marshal(a.logs)
		if err != nil {
			return nil, err
		}
		m["logs"] = b
	}
	return json.Marshal(m)
}

func (t *triager) output(g *triageGroup) (*triageGroupOut, error) {
	monitor := g.monRaw
	if monitor == nil {
		// 監視が消えたアラートは monitorId が null になる
		m := map[string]any{"id": nil, "type": g.monitor.Type}
		if g.monitorID != "" {
			m["id"] = g.monitorID
		}
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		monitor = b
	}
	o := &triageGroupOut{Status: g.status, OpenedAt: g.first, LastOpenedAt: g.last, Monitor: monitor}
	for _, a := range g.alerts {
		b, err := t.alertJSON(a)
		if err != nil {
			return nil, err
		}
		o.Alerts = append(o.Alerts, b)
	}
	if g.detailed {
		hostRank := map[string]int{}
		for i, a := range g.hosts {
			hostRank[a.HostID] = i
		}
		slices.SortStableFunc(g.metrics, func(x, y triageMetric) int {
			return cmp.Or(cmp.Compare(hostRank[x.HostID], hostRank[y.HostID]), cmp.Compare(x.Name, y.Name))
		})
		slices.SortStableFunc(g.otherMonitors, func(x, y triageOtherMonitor) int {
			return cmp.Or(cmp.Compare(hostRank[x.HostID], hostRank[y.HostID]), cmp.Compare(x.Name, y.Name), cmp.Compare(x.MonitorID, y.MonitorID))
		})
		// 複数のサービスに付いたアノテーションは 1 回だけ載せる
		seen := map[string]bool{}
		g.annotations = slices.DeleteFunc(g.annotations, func(raw json.RawMessage) bool {
			id := mackerel.StringField(raw, "id")
			dup := id != "" && seen[id]
			seen[id] = true
			return dup
		})
		slices.SortStableFunc(g.annotations, func(x, y json.RawMessage) int {
			var a, b struct {
				From int64  `json:"from"`
				ID   string `json:"id"`
			}
			_ = json.Unmarshal(x, &a)
			_ = json.Unmarshal(y, &b)
			return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.ID, b.ID))
		})
		ctx := &triageContext{Range: g.rng}
		src := t.opts.sources
		if src["metrics"] {
			ctx.Metrics = orEmpty(g.metrics)
		}
		if src["monitors"] {
			ctx.OtherMonitors = orEmpty(g.otherMonitors)
		}
		if src["annotations"] {
			ctx.Annotations = orEmpty(g.annotations)
		}
		if src["downtimes"] && g.downtimes != nil {
			ctx.Downtimes = g.downtimes
		}
		o.Context = ctx
	}
	o.Notes = t.notes(g)
	o.Next = t.next(g)
	slices.SortStableFunc(g.errors, func(x, y triageError) int {
		return cmp.Or(cmp.Compare(x.Source, y.Source), cmp.Compare(x.Message, y.Message))
	})
	o.Errors = orEmpty(g.errors)
	return o, nil
}

// orEmpty returns s, or an empty slice for nil, so that it prints as [].
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ---- command ----

func newTriageCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		o                    triageOptions
		statuses, skip, only []string
		window, nowStr       string
		service, role        string
		hostRef, monitorRef  string
		otelServices         []string
		maxGroups, maxHosts  int
		maxSamples           int
	)
	cmd := &cobra.Command{
		Use:   "triage [<alert-id>...]",
		Short: "Gather the context of open alerts, for the first minutes of an incident",
		Long: withAPI(`Gather, in one read-only call, what to look at first when alerts fire: the
open alerts grouped by monitor, and around each group the hosts, the other
monitors failing on them, the monitored metrics, graph annotations (deploys)
and downtimes in effect. Call it first when handling an incident; it changes
nothing, and it reports facts and rule-based notes, not a diagnosis.

Groups are ordered by their heaviest status (CRITICAL, WARNING, UNKNOWN),
then by their newest alert. Only the first --max-groups groups get
"context"; the rest list their alerts and the notes that need no extra
request. Context is read in [t - --window, min(t + --window, now)], where t
is the group's earliest alert, for up to --max-hosts hosts per group (the
newest alerts first).

Give alert IDs ("-" reads them from stdin) to look at those alerts only,
closed ones included; each alert then also has its whole status history
("logs", up to 50 changes).

Output:
  {"generatedAt", "summary": {"alerts", "open", "critical", "warning",
   "unknown", "groups"}, "groups": [...], "otel": [...]}
Each group:
  "status", "openedAt" (earliest), "lastOpenedAt" (newest),
  "monitor"   the monitor as the API returns it
  "alerts"    the alerts as the API returns them, newest first, with
              "host": {"id", "name", "status", "roles", "isRetired",
              "cpuCount", "lastMetricAt" (connectivity alerts)}, and
              "statusChangedAt" (alerts whose hosts get context)
  "context"   {"range": {"from", "to"}, "metrics": [{"hostId" or "service",
              "name", "points"}], "otherMonitors": [{"hostId", "monitorId",
              "name", "type", "status"}], "annotations", "downtimes"}
  "notes"     [{"kind", "message", "resource"}]
  "next"      mcrl commands to look further, ready to run
  "errors"    [{"source", "message"}] for what could not be read
A source turned off with --skip or --only is left out of "context".

An alert's "value" comes from the API as is. For an open alert it can
change over time, so it may be newer than the metrics in "context", which
stay around the earliest "openedAt". "statusChangedAt" (epoch seconds) is the
time of the alert's latest status change, read from the newest entry of its
status history; for an alert that never changed status it equals "openedAt".
The note status-changed is added when that time is outside the context
range; "next" then has commands that read the metrics around the change and
over the last --window.
The history is read only for the alerts whose hosts get context (up to
--max-hosts per group, newest first; for alerts without a host, the newest
--max-hosts of the group), or for all alerts when alert IDs are given. The
other alerts have no "statusChangedAt" and no such note.

Metrics: host monitors read the monitored metric of each host, service
monitors the service metric. Rate monitors are not computed; their source
metrics are shown instead (the alert's "value" has the rate):
  cpu%     cpu.idle.percentage (summed over cores; divide by cpuCount)
  memory%  memory.used, memory.total
  swap%    memory.swap_used (or memory.swap_free), memory.swap_total
  disk%    filesystem.*.used, filesystem.*.size
container-cpu% and container-memory% have no known source metrics and get
an "errors" entry.

Notes:
  monitor-muted           the monitor is muted
  in-downtime             a downtime in effect now covers the monitor; since
                          downtimes stop new alerts, it most likely started
                          after the alert opened
  host-retired            the alert's host is retired or deleted
  host-not-working        the host is standby, maintenance or poweroff
  metrics-stopped         connectivity alert whose host last sent metrics
                          before the alert opened. The time comes from
                          GET /tsdb/latest; for a host that has these metrics
                          there only with null values, from the last point
                          of loadavg5, cpu.user.percentage or memory.used
                          (the first that has points) from 7 days before
                          the alert opened until now. Over a day or more the
                          points are aggregated, so the time is approximate.
                          A last point after the alert opened gives no note.
                          With no point, or for a host /tsdb/latest knows
                          none of them for, there is no note.
  status-changed          an alert's latest status change is outside the
                          context range
  recent-annotation       a graph annotation in the range
  many-hosts              two or more hosts alert on the monitor
  other-monitors-failing  other monitors are not OK on the host

Logs and traces: OpenTelemetry service names are not Mackerel services or
roles, so they are read only for --otel-service, into "otel", in the range
around the earliest alert of the detailed groups: up to --max-samples error
logs (newest first; bodies are cut at 2000 bytes with "truncated": true)
and error traces (slowest first). Traces are limited to 30 minutes, so
keep --window at 15m or less to get them.

Requests run 4 at a time. When a request fails, the rest is still returned
with the failure in "errors", and the exit code is 0.`, "Read",
			"GET /api/v0/alerts", "GET /api/v0/alerts/<id> (alert IDs)", "GET /api/v0/alerts/<id>/logs",
			"GET /api/v0/monitors", "GET /api/v0/hosts", "GET /api/v0/hosts/<id>", "GET /api/v0/tsdb/latest",
			"GET /api/v0/hosts/<id>/monitored-statuses", "GET /api/v0/hosts/<id>/metrics", "GET /api/v0/hosts/<id>/metric-names",
			"GET /api/v0/services/<name>/metrics", "GET /api/v0/services/<name>/metric-names",
			"GET /api/v0/graph-annotations", "GET /api/v0/downtimes",
			"POST /api/v0/logs (--otel-service)", "POST /api/v0/traces (--otel-service)"),
		Example: `  mcrl triage
  mcrl triage exampleAlr1
  mcrl triage --service web --status critical
  mcrl triage --host web-3 --window 30m --skip annotations
  mcrl triage --otel-service checkout-api,payment
  mcrl triage --jq '.groups[] | [.status, .monitor.name, (.alerts | length)] | @tsv'`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if len(args) > 0 {
				if o.ids, err = expandArgs(f, args); err != nil {
					return err
				}
			}
			for _, s := range statuses {
				s = strings.ToUpper(strings.TrimSpace(s))
				if !contains(triageStatuses, s) {
					return cmdutil.Usagef("invalid --status %q (valid: %s)", s, strings.Join(triageStatuses, ", "))
				}
				o.statuses = append(o.statuses, s)
			}
			if o.sources, err = triageSourceSet(skip, only); err != nil {
				return err
			}
			if o.window, err = timeexpr.ParseDuration(window); err != nil || o.window <= 0 {
				return cmdutil.Usagef("invalid --window %q (e.g. 15m, 1h)", window)
			}
			for name, v := range map[string]int{"max-groups": maxGroups, "max-hosts": maxHosts, "max-samples": maxSamples} {
				if v < 1 {
					return cmdutil.Usagef("--%s must be 1 or more", name)
				}
			}
			o.maxGroups, o.maxHosts, o.maxSamples = maxGroups, maxHosts, maxSamples
			for _, s := range otelServices {
				if s = strings.TrimSpace(s); s != "" && !contains(o.otelServices, s) {
					o.otelServices = append(o.otelServices, s)
				}
			}
			o.service = service
			if role != "" {
				svc, r := splitScope(role)
				if svc == "" || r == "" {
					return cmdutil.Usagef("invalid --role %q (want <service>:<role>)", role)
				}
				o.role = svc + ":" + r
			}
			o.now = time.Now()
			if nowStr != "" {
				if o.now, err = timeexpr.Parse(nowStr, o.now, time.Local); err != nil {
					return cmdutil.Usagef("invalid --now: %v", err)
				}
			}
			ctx := cmd.Context()
			c, err := apiClient(ctx, f)
			if err != nil {
				return err
			}
			if hostRef != "" {
				if o.host, err = c.ResolveHostID(ctx, hostRef); err != nil {
					return err
				}
			}
			if monitorRef != "" {
				if o.monitor, err = c.ResolveID(ctx, mackerel.KindMonitor, monitorRef); err != nil {
					return err
				}
			}
			out, err := (&triager{ctx: ctx, c: c, opts: o}).run()
			if err != nil {
				return err
			}
			b, err := json.Marshal(out)
			if err != nil {
				return err
			}
			return printRaw(f, b)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&service, "service", "", "only alerts of this service (the monitor's service or the host's roles)")
	fl.StringVar(&role, "role", "", "only alerts of hosts in this role (<service>:<role>)")
	fl.StringVar(&hostRef, "host", "", "only alerts of this host (ID or name)")
	fl.StringVar(&monitorRef, "monitor", "", "only alerts of this monitor (ID or name)")
	fl.StringSliceVar(&statuses, "status", nil, "only alerts in this status: CRITICAL, WARNING, UNKNOWN (repeatable or comma-separated)")
	fl.StringVar(&window, "window", "15m", "how long before and after the alert to read context for")
	fl.IntVar(&maxGroups, "max-groups", 5, "number of groups (monitors) to read context for")
	fl.IntVar(&maxHosts, "max-hosts", 3, "number of hosts per group to read metrics and monitor statuses for")
	fl.IntVar(&maxSamples, "max-samples", 5, "number of logs and traces per --otel-service")
	fl.StringSliceVar(&skip, "skip", nil, "do not read these: "+strings.Join(triageSources, ", "))
	fl.StringSliceVar(&only, "only", nil, "read only these: "+strings.Join(triageSources, ", "))
	fl.StringSliceVar(&otelServices, "otel-service", nil, "OpenTelemetry service (service.name) to read error logs and traces of (repeatable or comma-separated)")
	fl.StringVar(&nowStr, "now", "", "judge as of this time instead of now")
	_ = fl.MarkHidden("now")
	cmd.MarkFlagsMutuallyExclusive("skip", "only")
	_ = cmd.RegisterFlagCompletionFunc("status", cobra.FixedCompletions(triageStatuses, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("skip", cobra.FixedCompletions(triageSources, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("only", cobra.FixedCompletions(triageSources, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("host", completeHosts(f))
	_ = cmd.RegisterFlagCompletionFunc("monitor", completeKind(f, mackerel.KindMonitor))
	_ = cmd.RegisterFlagCompletionFunc("service", func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeFromList(cmd, f, "/services", "services", nil, "name", "memo"), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// triageSourceSet turns --skip / --only into the set of sources to read.
func triageSourceSet(skip, only []string) (map[string]bool, error) {
	set := map[string]bool{}
	for _, s := range triageSources {
		set[s] = len(only) == 0
	}
	for _, list := range [][]string{skip, only} {
		for _, s := range list {
			s = strings.TrimSpace(s)
			if !contains(triageSources, s) {
				return nil, cmdutil.Usagef("unknown source %q (valid: %s)", s, strings.Join(triageSources, ", "))
			}
		}
	}
	for _, s := range skip {
		set[strings.TrimSpace(s)] = false
	}
	for _, s := range only {
		set[strings.TrimSpace(s)] = true
	}
	return set, nil
}
