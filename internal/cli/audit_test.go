package cli

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// auditFixture builds an auditor whose lists are already loaded, so the
// checks run without an API client.
func auditFixture(t *testing.T, now time.Time, lists map[string]string, latest map[string]int64) *auditor {
	t.Helper()
	a := newAuditor(t.Context(), nil, now, 30*24*time.Hour)
	for path, body := range lists {
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		a.lists[path] = items
	}
	a.latest = latest
	return a
}

func findingIDs(t *testing.T, check func() ([]auditFinding, error)) []string {
	t.Helper()
	fs, err := check()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, f := range fs {
		ids = append(ids, f.Resource.ID)
	}
	return ids
}

func TestAuditEndedDowntimes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	a := auditFixture(t, now, map[string]string{"/downtimes": `[
		{"id": "ended", "start": 1799996400, "duration": 60},
		{"id": "ends-now", "start": 1799999940, "duration": 1},
		{"id": "ends-in-1s", "start": 1799999941, "duration": 1},
		{"id": "recurring-forever", "start": 1700000000, "duration": 60, "recurrence": {"type": "daily", "interval": 1}},
		{"id": "recurring-ended", "start": 1700000000, "duration": 60, "recurrence": {"type": "daily", "interval": 1, "until": 1799996400}},
		{"id": "recurring-last-running", "start": 1700000000, "duration": 60, "recurrence": {"type": "daily", "interval": 1, "until": 1799996401}}
	]`}, nil)
	got := findingIDs(t, a.endedDowntimes)
	want := []string{"ended", "ends-now", "recurring-ended"}
	if !slices.Equal(got, want) {
		t.Errorf("ended downtimes = %v, want %v", got, want)
	}
}

func TestAuditHostsByMetrics(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cutoff := now.Add(-30 * 24 * time.Hour).Unix()
	a := auditFixture(t, now, map[string]string{"/hosts": `[
		{"id": "working-fresh", "status": "working", "createdAt": 1, "roles": {"web": ["app"]}},
		{"id": "working-at-cutoff", "status": "working", "createdAt": 1, "roles": {}},
		{"id": "working-after-cutoff", "status": "working", "createdAt": 1},
		{"id": "working-no-agent", "status": "working", "createdAt": 1},
		{"id": "poweroff-old", "status": "poweroff", "createdAt": 1, "roles": {"web": []}},
		{"id": "standby-fresh", "status": "standby", "createdAt": 1},
		{"id": "maintenance-new-no-agent", "status": "maintenance", "createdAt": 1799990000},
		{"id": "maintenance-old-no-agent", "status": "maintenance", "createdAt": 1000}
	]`}, map[string]int64{
		"working-fresh":        now.Unix() - 60,
		"working-at-cutoff":    cutoff,
		"working-after-cutoff": cutoff + 1,
		"poweroff-old":         cutoff - 100,
		"standby-fresh":        now.Unix() - 60,
	})
	if got, want := findingIDs(t, a.staleMetricHosts), []string{"working-at-cutoff"}; !slices.Equal(got, want) {
		t.Errorf("stale = %v, want %v", got, want)
	}
	if got, want := findingIDs(t, a.inactiveHosts), []string{"poweroff-old", "maintenance-old-no-agent"}; !slices.Equal(got, want) {
		t.Errorf("inactive = %v, want %v", got, want)
	}
	want := []string{"working-at-cutoff", "working-after-cutoff", "working-no-agent", "poweroff-old", "standby-fresh", "maintenance-new-no-agent", "maintenance-old-no-agent"}
	if got := findingIDs(t, a.hostsWithoutRole); !slices.Equal(got, want) {
		t.Errorf("without role = %v, want %v", got, want)
	}
}

func TestAuditReferences(t *testing.T) {
	a := auditFixture(t, time.Unix(1_800_000_000, 0), map[string]string{
		"/services": `[{"name": "web", "roles": ["app", "idle"]}, {"name": "empty", "roles": []}]`,
		"/hosts":    `[{"id": "h1", "status": "working", "roles": {"web": ["app"]}}]`,
		"/monitors": `[
			{"id": "m-ok", "type": "host", "scopes": ["web", "web: app"], "excludeScopes": []},
			{"id": "m-bad", "type": "host", "isMute": true, "scopes": ["gone", "web: gone"], "excludeScopes": ["web:app"]},
			{"id": "m-svc", "type": "service", "service": "gone"}
		]`,
		"/downtimes": `[{"id": "d1", "start": 1, "duration": 1, "serviceScopes": ["web"], "roleScopes": ["web: nope"], "monitorScopes": ["m-ok", "m-nope"]}]`,
		"/channels":  `[{"id": "c-used"}, {"id": "c-unused"}]`,
		"/notification-groups": `[
			{"id": "g1", "childChannelIds": ["c-used"], "childNotificationGroupIds": []},
			{"id": "g-empty", "childChannelIds": [], "childNotificationGroupIds": []},
			{"id": "g-parent", "childChannelIds": [], "childNotificationGroupIds": ["g1"]}
		]`,
		"/users":       `[{"id": "u1", "isMFAEnabled": true}, {"id": "u2", "isMFAEnabled": false}]`,
		"/invitations": `[{"email": "old", "expiresAt": 1799999999}, {"email": "now", "expiresAt": 1800000000}, {"email": "new", "expiresAt": 1800000001}]`,
	}, nil)

	if got, want := findingIDs(t, a.mutedMonitors), []string{"m-bad"}; !slices.Equal(got, want) {
		t.Errorf("muted = %v, want %v", got, want)
	}
	fs, err := a.danglingScopes()
	var got []string
	for _, f := range fs {
		got = append(got, f.Resource.ID+" "+f.Details["field"].(string)+" "+f.Details["scope"].(string))
	}
	want := []string{
		"d1 monitorScopes m-nope",
		"d1 roleScopes web: nope",
		"m-bad scopes gone",
		"m-bad scopes web: gone",
		"m-svc service gone",
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("dangling = %v (%v), want %v", got, err, want)
	}
	if got, want := findingIDs(t, a.unusedRoles), []string{"web:idle"}; !slices.Equal(got, want) {
		t.Errorf("unused roles = %v, want %v", got, want)
	}
	if got, want := findingIDs(t, a.unusedChannels), []string{"c-unused"}; !slices.Equal(got, want) {
		t.Errorf("unused channels = %v, want %v", got, want)
	}
	if got, want := findingIDs(t, a.emptyNotificationGroups), []string{"g-empty"}; !slices.Equal(got, want) {
		t.Errorf("empty groups = %v, want %v", got, want)
	}
	if got, want := findingIDs(t, a.usersWithoutMFA), []string{"u2"}; !slices.Equal(got, want) {
		t.Errorf("users = %v, want %v", got, want)
	}
	if got, want := findingIDs(t, a.expiredInvitations), []string{"old", "now"}; !slices.Equal(got, want) {
		t.Errorf("invitations = %v, want %v", got, want)
	}
}

func TestSelectAuditChecks(t *testing.T) {
	cs, err := selectAuditChecks([]string{"user-without-mfa", "muted-monitor"})
	if err != nil || len(cs) != 2 || cs[0].kind != "muted-monitor" {
		t.Errorf("select = %v, %v", cs, err)
	}
	if _, err := selectAuditChecks([]string{"nope"}); err == nil {
		t.Error("unknown kind accepted")
	}
}
