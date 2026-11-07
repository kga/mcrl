package main

import (
	"io"
	"net/http"
)

// serveAuditFixture answers for an organization full of things mcrl audit
// should report (cmd/mcrl/testdata/script/audit.txtar). The tests run with
// --now @1800000000; 30 days before is 1797408000.
func serveAuditFixture(w http.ResponseWriter, r *http.Request) bool {
	bodies := map[string]string{
		"GET /api/v0/monitors": `{"monitors":[
			{"id":"monOK000001","name":"cpu","type":"host","isMute":false,"scopes":["web: app"],"excludeScopes":[]},
			{"id":"monMUTED001","name":"muted disk","type":"host","isMute":true,"scopes":["gone"],"excludeScopes":[]}]}`,
		"GET /api/v0/downtimes": `{"downtimes":[
			{"id":"dtENDED0001","name":"last week","start":1799395200,"duration":60},
			{"id":"dtENDSNOW01","name":"ends now","start":1799999940,"duration":1},
			{"id":"dtWEEKLY001","name":"weekly","start":1700000000,"duration":60,"recurrence":{"type":"weekly","interval":1}}]}`,
		"GET /api/v0/services": `{"services":[
			{"name":"web","memo":"","roles":["app","idle"]},
			{"name":"batch","memo":"","roles":[]},
			{"name":"external","memo":"","roles":[]}]}`,
		"GET /api/v0/services/batch/metric-names":    `{"names":[]}`,
		"GET /api/v0/services/external/metric-names": `{"names":["response.time"]}`,
		"GET /api/v0/hosts": `{"hosts":[
			{"id":"hostWEB0001","name":"web-1","status":"working","createdAt":1700000000,"roles":{"web":["app"]}},
			{"id":"hostSTALE01","name":"stale-1","status":"working","createdAt":1700000000,"roles":{"web":["app"]}},
			{"id":"hostOFF0001","name":"off-1","status":"poweroff","createdAt":1700000000,"roles":{}}]}`,
		"GET /api/v0/tsdb/latest": `{"tsdbLatest":{
			"hostWEB0001":{"loadavg5":{"time":1799999940,"value":0.1},"memory.used":{"time":1799999880,"value":1}},
			"hostSTALE01":{"loadavg5":{"time":1797408000,"value":0.1}}}}`,
		"GET /api/v0/alerts": `{"alerts":[
			{"id":"alRETIRED01","status":"CRITICAL","monitorId":"chk","type":"check","hostId":"hostRETIRED","openedAt":1799000000},
			{"id":"alLIVE00001","status":"WARNING","monitorId":"monOK000001","type":"host","hostId":"hostWEB0001","openedAt":1799000000}]}`,
		"GET /api/v0/hosts/hostRETIRED": `{"host":{"id":"hostRETIRED","name":"old-1","status":"working","isRetired":true,"retiredAt":1798000000,"roles":{}}}`,
		"GET /api/v0/channels": `{"channels":[
			{"id":"chUSED00001","name":"ops","type":"slack"},
			{"id":"chUNUSED001","name":"old hook","type":"webhook"}]}`,
		"GET /api/v0/notification-groups": `{"notificationGroups":[
			{"id":"ngDEFAULT01","name":"Default","type":"group-default","childNotificationGroupIds":[],"childChannelIds":["chUSED00001"]},
			{"id":"ngEMPTY0001","name":"nobody","type":"group","childNotificationGroupIds":[],"childChannelIds":[]}]}`,
		"GET /api/v0/users": `{"users":[
			{"id":"userMFA0001","screenName":"alice","email":"alice@example.com","authority":"owner","isMFAEnabled":true,"authenticationMethods":["password"]},
			{"id":"userNOMFA01","screenName":"bob","email":"bob@example.com","authority":"manager","isMFAEnabled":false,"authenticationMethods":["password"]}]}`,
		"GET /api/v0/invitations": `{"invitations":[
			{"email":"late@example.com","authority":"viewer","expiresAt":1800000000},
			{"email":"pending@example.com","authority":"viewer","expiresAt":1800000001}]}`,
	}
	body, ok := bodies[r.Method+" "+r.URL.Path]
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, body)
	return true
}
