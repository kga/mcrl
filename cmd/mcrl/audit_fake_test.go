package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
			{"id":"hostOFF0001","name":"off-1","status":"poweroff","createdAt":1700000000,"roles":{}},
			{"id":"hostSLOW0001","name":"slow-1","status":"standby","createdAt":1700000000,"roles":{"web":["app"]}},
			{"id":"hostGONE0001","name":"gone-1","status":"working","createdAt":1700000000,"roles":{"web":["app"]}},
			{"id":"hostQUIET001","name":"quiet-1","status":"standby","createdAt":1700000000,"roles":{"web":["app"]}}]}`,
		"GET /api/v0/tsdb/latest": `{"tsdbLatest":{
			"hostWEB0001":{"loadavg5":{"time":1799999940,"value":0.1},"memory.used":{"time":1799999880,"value":1}},
			"hostSTALE01":{"loadavg5":{"time":1797408000,"value":0.1}},
			"hostSLOW0001":{"cpu.user.percentage":null,"memory.used":null},
			"hostGONE0001":{"loadavg5":null,"cpu.user.percentage":null,"memory.used":null},
			"hostQUIET001":{"loadavg5":null,"cpu.user.percentage":null,"memory.used":null}}}`,
		// hostOFF0001 は /tsdb/latest に出てこない（API だけで登録したホスト）
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
	// 時系列 API: /tsdb/latest が null を返すホストの最後の点は、そちらにだけ残っている。
	// hostSLOW0001 は Windows のように loadavg5 がなく（/tsdb/latest にも出ない）、約 1 日前の点が
	// cpu.user.percentage にある。hostQUIET001 と hostGONE0001 は範囲内に点がない。from から to の範囲の点だけを返す
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v0/hosts/") && strings.HasSuffix(r.URL.Path, "/metrics") {
		q := r.URL.Query()
		from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
		to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v0/hosts/hostSLOW0001/metrics" && q.Get("name") == "loadavg5" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"metric not found"}}`)
			return true
		}
		points := []string{}
		if r.URL.Path == "/api/v0/hosts/hostSLOW0001/metrics" && q.Get("name") == "cpu.user.percentage" {
			for _, t := range []int64{1799890000, 1799900000} {
				if from <= t && t <= to {
					points = append(points, fmt.Sprintf(`{"time":%d,"value":1}`, t))
				}
			}
		}
		io.WriteString(w, `{"metrics":[`+strings.Join(points, ",")+`]}`)
		return true
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
