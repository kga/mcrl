package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// serveTriageFixture answers for an organization in the middle of an
// incident (cmd/mcrl/testdata/script/triage.txtar). The tests run with
// --now @1800000000.
//
// Open alerts, in the order mcrl triage ranks their groups:
//
//	monCONN0001 CRITICAL  off-1 (poweroff), metrics stopped before the alert
//	monCPU00001 CRITICAL  web-1, cpu%, muted monitor
//	monDISK0001 CRITICAL  web-2, web-1, web-3 (newest first), disk%;
//	                      alDISK00002 opened CRITICAL and changed to WARNING
//	                      after the context range, the others never changed
//	monCTR00001 WARNING   retired host, container-cpu%
//	monSVC00001 WARNING   service monitor of web (its status history is denied)
//	monSWAP0001 WARNING   web-2, swap% (the host has no memory.swap_used)
func serveTriageFixture(w http.ResponseWriter, r *http.Request, reqBody []byte) bool {
	writeJSON := func(status int, body string) bool {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
		return true
	}
	bodies := map[string]string{
		"GET /api/v0/monitors": `{"monitors":[
			{"id":"monCONN0001","name":"connectivity","type":"connectivity","isMute":false,"scopes":[],"excludeScopes":[]},
			{"id":"monCPU00001","name":"cpu high","type":"host","isMute":true,"metric":"cpu%","operator":">","warning":80,"critical":90,"scopes":["web"]},
			{"id":"monDISK0001","name":"disk usage","type":"host","isMute":false,"metric":"disk%","operator":">","warning":80,"critical":90,"scopes":["web"]},
			{"id":"monCTR00001","name":"container cpu","type":"host","isMute":false,"metric":"container-cpu%","operator":">","warning":80,"critical":90},
			{"id":"monSVC00001","name":"latency","type":"service","isMute":false,"service":"web","metric":"response.time","operator":">","warning":500,"critical":1000},
			{"id":"monSWAP0001","name":"swap","type":"host","isMute":false,"metric":"swap%","operator":">","warning":50,"critical":80}]}`,
		"GET /api/v0/alerts": `{"alerts":[
			{"id":"alSWAP00001","status":"WARNING","monitorId":"monSWAP0001","type":"host","hostId":"hostWEB0002","value":60,"openedAt":1799999100},
			{"id":"alDISK00003","status":"CRITICAL","monitorId":"monDISK0001","type":"host","hostId":"hostWEB0003","value":91,"openedAt":1799999000},
			{"id":"alDISK00001","status":"CRITICAL","monitorId":"monDISK0001","type":"host","hostId":"hostWEB0001","value":96.2,"openedAt":1799999400},
			{"id":"alSVC000001","status":"WARNING","monitorId":"monSVC00001","type":"service","value":700,"openedAt":1799999300},
			{"id":"alDISK00002","status":"WARNING","monitorId":"monDISK0001","type":"host","hostId":"hostWEB0002","value":85,"openedAt":1799999500},
			{"id":"alCPU000001","status":"CRITICAL","monitorId":"monCPU00001","type":"host","hostId":"hostWEB0001","value":95,"openedAt":1799999600},
			{"id":"alCONN00001","status":"CRITICAL","monitorId":"monCONN0001","type":"connectivity","hostId":"hostOFF0001","message":"no metric","openedAt":1799999700},
			{"id":"alCTR000001","status":"WARNING","monitorId":"monCTR00001","type":"host","hostId":"hostRETIRED","value":88,"openedAt":1799999800}]}`,
		"GET /api/v0/alerts/alDISK00001":      `{"id":"alDISK00001","status":"CRITICAL","monitorId":"monDISK0001","type":"host","hostId":"hostWEB0001","value":96.2,"openedAt":1799999400}`,
		"GET /api/v0/alerts/alDISK00001/logs": `{"logs":[{"id":"log1","status":"CRITICAL","trigger":"monitoring","createdAt":1799999400}]}`,
		// 先頭の CRITICAL から、範囲（〜1799999900）の外で WARNING に変わった
		"GET /api/v0/alerts/alDISK00002/logs": `{"logs":[
			{"id":"log3","status":"WARNING","trigger":"monitoring","createdAt":1799999950},
			{"id":"log2","status":"CRITICAL","trigger":"monitoring","createdAt":1799999500}]}`,
		"GET /api/v0/hosts": `{"hosts":[
			{"id":"hostWEB0001","name":"web-1","status":"working","roles":{"web":["app"]},"meta":{"cpu":[{},{}]}},
			{"id":"hostWEB0002","name":"web-2","status":"working","roles":{"web":["app"]}},
			{"id":"hostWEB0003","name":"web-3","status":"working","roles":{"web":["app"]}},
			{"id":"hostOFF0001","name":"off-1","status":"poweroff","roles":{}},
			{"id":"hostSLOW0001","name":"slow-1","status":"poweroff","roles":{}},
			{"id":"hostNONE0001","name":"none-1","status":"poweroff","roles":{}},
			{"id":"hostGHOST001","name":"ghost-1","status":"poweroff","roles":{}},
			{"id":"hostFAIL0001","name":"fail-1","status":"poweroff","roles":{}},
			{"id":"hostLATE0001","name":"late-1","status":"poweroff","roles":{}}]}`,
		"GET /api/v0/hosts/hostRETIRED": `{"host":{"id":"hostRETIRED","name":"old-1","status":"working","isRetired":true,"retiredAt":1799990000,"roles":{}}}`,
		"GET /api/v0/tsdb/latest": `{"tsdbLatest":{"hostOFF0001":{"loadavg5":{"time":1799999640,"value":0.1}},
			"hostSLOW0001":{"cpu.user.percentage":null,"memory.used":null},
			"hostNONE0001":{"loadavg5":null,"cpu.user.percentage":null,"memory.used":null},
			"hostLATE0001":{"loadavg5":null,"cpu.user.percentage":null,"memory.used":null},
			"hostFAIL0001":{"loadavg5":null,"cpu.user.percentage":null,"memory.used":null}}}`,
		// 疎通監視のアラート。ID を指定したときだけ読む（一覧には出ない）。
		// hostSLOW0001 は loadavg5 が /tsdb/latest になく、最後の点は時系列 API にだけ残っており、hostNONE0001 には点がない
		"GET /api/v0/alerts/alCONN00002":      `{"id":"alCONN00002","status":"CRITICAL","monitorId":"monCONN0001","type":"connectivity","hostId":"hostSLOW0001","openedAt":1799999700}`,
		"GET /api/v0/alerts/alCONN00003":      `{"id":"alCONN00003","status":"CRITICAL","monitorId":"monCONN0001","type":"connectivity","hostId":"hostNONE0001","openedAt":1799999700}`,
		"GET /api/v0/alerts/alCONN00004":      `{"id":"alCONN00004","status":"CRITICAL","monitorId":"monCONN0001","type":"connectivity","hostId":"hostGHOST001","openedAt":1799999700}`,
		"GET /api/v0/alerts/alCONN00004/logs": `{"logs":[]}`,
		"GET /api/v0/alerts/alCONN00005":      `{"id":"alCONN00005","status":"CRITICAL","monitorId":"monCONN0001","type":"connectivity","hostId":"hostFAIL0001","openedAt":1799999700}`,
		"GET /api/v0/alerts/alCONN00005/logs": `{"logs":[]}`,
		// 閉じたアラート。アラートが開いたあとにも点がある（hostLATE0001）
		"GET /api/v0/alerts/alCONN00006":      `{"id":"alCONN00006","status":"OK","monitorId":"monCONN0001","type":"connectivity","hostId":"hostLATE0001","openedAt":1799999700,"closedAt":1799999900}`,
		"GET /api/v0/alerts/alCONN00006/logs": `{"logs":[]}`,
		"GET /api/v0/alerts/alCONN00002/logs": `{"logs":[]}`,
		"GET /api/v0/alerts/alCONN00003/logs": `{"logs":[]}`,
		"GET /api/v0/hosts/hostWEB0001/monitored-statuses": `{"monitoredStatuses":[
			{"monitorId":"monCPU00001","status":"CRITICAL"},{"monitorId":"monDISK0001","status":"CRITICAL"},{"monitorId":"monCONN0001","status":"OK"}]}`,
		"GET /api/v0/hosts/hostWEB0002/monitored-statuses": `{"monitoredStatuses":[
			{"monitorId":"monDISK0001","status":"WARNING"},{"monitorId":"monSWAP0001","status":"WARNING"}]}`,
		"GET /api/v0/hosts/hostOFF0001/monitored-statuses": `{"monitoredStatuses":[{"monitorId":"monCONN0001","status":"CRITICAL"}]}`,
		"GET /api/v0/downtimes": `{"downtimes":[
			{"id":"dtWEB000001","name":"web maintenance","start":1799999000,"duration":60,"serviceScopes":["web"]},
			{"id":"dtBATCH0001","name":"batch maintenance","start":1799999000,"duration":60,"serviceScopes":["batch"]},
			{"id":"dtENDED0001","name":"last week","start":1799000000,"duration":60}]}`,
	}
	route := r.Method + " " + r.URL.Path
	if route == "GET /api/v0/hosts" && r.URL.Query().Get("name") == "web-1" {
		return writeJSON(http.StatusOK, `{"hosts":[{"id":"hostWEB0001","name":"web-1","status":"working","roles":{"web":["app"]}}]}`)
	}
	if body, ok := bodies[route]; ok {
		return writeJSON(http.StatusOK, body)
	}
	q := r.URL.Query()
	switch {
	case route == "GET /api/v0/alerts/alSVC000001/logs":
		// 経過の取得の失敗: アラートは返り、errors に入る
		return writeJSON(http.StatusForbidden, `{"error":{"message":"Forbidden"}}`)
	case strings.HasPrefix(route, "GET /api/v0/alerts/") && strings.HasSuffix(route, "/logs"):
		// 状態が変わっていないアラート: 経過は開いたときの 1 件だけ
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v0/alerts/"), "/logs")
		openedAt := map[string]string{
			"alSWAP00001": "1799999100", "alDISK00003": "1799999000", "alCPU000001": "1799999600",
			"alCONN00001": "1799999700", "alCTR000001": "1799999800",
		}[id]
		if openedAt == "" {
			return false
		}
		return writeJSON(http.StatusOK, `{"logs":[{"id":"log-`+id+`","status":"CRITICAL","trigger":"monitoring","createdAt":`+openedAt+`}]}`)
	case route == "GET /api/v0/hosts/hostWEB0003/monitored-statuses":
		// 一部の取得の失敗: 他の情報は返り、errors に入る
		return writeJSON(http.StatusForbidden, `{"error":{"message":"Forbidden"}}`)
	case strings.HasPrefix(route, "GET /api/v0/hosts/") && strings.HasSuffix(route, "/metric-names"):
		return writeJSON(http.StatusOK, `{"names":["cpu.idle.percentage","filesystem.root.size","filesystem.root.used","filesystem.vdb.size","filesystem.vdb.used","memory.swap_free","memory.swap_total"]}`)
	case strings.HasPrefix(route, "GET /api/v0/hosts/") && strings.HasSuffix(route, "/metrics"),
		route == "GET /api/v0/services/web/metrics":
		if q.Get("from") == "" || q.Get("to") == "" {
			return writeJSON(http.StatusBadRequest, `{"error":{"message":"Bad request."}}`)
		}
		if strings.HasPrefix(route, "GET /api/v0/hosts/hostSLOW0001/") {
			// cpu.user.percentage の最後の点は 1799990000（約 2.7 時間前）。範囲に入る点だけ返す
			from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
			to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
			if q.Get("name") == "cpu.user.percentage" && from <= 1799990000 && 1799990000 <= to {
				return writeJSON(http.StatusOK, `{"metrics":[{"time":1799980000,"value":1},{"time":1799990000,"value":2}]}`)
			}
			return writeJSON(http.StatusOK, `{"metrics":[]}`)
		}
		if strings.HasPrefix(route, "GET /api/v0/hosts/hostLATE0001/") {
			from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
			to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
			if q.Get("name") == "loadavg5" && from <= 1799999800 && 1799999800 <= to {
				return writeJSON(http.StatusOK, `{"metrics":[{"time":1799990000,"value":1},{"time":1799999800,"value":2}]}`)
			}
			return writeJSON(http.StatusOK, `{"metrics":[]}`)
		}
		if strings.HasPrefix(route, "GET /api/v0/hosts/hostFAIL0001/") {
			return writeJSON(http.StatusInternalServerError, `{"error":{"message":"Internal error"}}`)
		}
		if strings.HasPrefix(route, "GET /api/v0/hosts/hostNONE0001/") {
			return writeJSON(http.StatusOK, `{"metrics":[]}`)
		}
		if q.Get("name") == "memory.swap_used" || strings.Contains(q.Get("name"), "%") {
			return writeJSON(http.StatusNotFound, `{"error":{"message":"metric not found"}}`)
		}
		return writeJSON(http.StatusOK, `{"metrics":[{"time":1799999460,"value":91},{"time":1799999520,"value":92}]}`)
	case route == "GET /api/v0/graph-annotations":
		if q.Get("service") != "web" || q.Get("from") == "" || q.Get("to") == "" {
			return writeJSON(http.StatusOK, `{"graphAnnotations":[]}`)
		}
		return writeJSON(http.StatusOK, `{"graphAnnotations":[{"id":"anWEB000001","title":"deploy a1b2c3d","description":"","service":"web","from":1799999200,"to":1799999260}]}`)
	case route == "POST /api/v0/logs":
		var b struct{ ServiceName string }
		_ = json.Unmarshal(reqBody, &b)
		if b.ServiceName == "denied" {
			return writeJSON(http.StatusForbidden, `{"error":{"message":"Forbidden"}}`)
		}
		long := strings.Repeat("x", 2500)
		return writeJSON(http.StatusOK, `{"results":[
			{"cursor":"c1","timestamp":"2026-01-15T08:00:00Z","severity":"ERROR","body":"`+long+`","resourceAttributes":{"service.name":"`+b.ServiceName+`"}},
			{"cursor":"c2","timestamp":"2026-01-15T07:59:00Z","severity":"FATAL","body":"short","resourceAttributes":{}}],
			"pagination":{"hasNextPage":false,"hasPreviousPage":false,"startCursor":"c1","endCursor":"c2"}}`)
	case route == "POST /api/v0/traces":
		var b struct{ ServiceName string }
		_ = json.Unmarshal(reqBody, &b)
		if b.ServiceName == "denied" {
			return writeJSON(http.StatusForbidden, `{"error":{"message":"Forbidden"}}`)
		}
		return writeJSON(http.StatusOK, `{"results":[{"traceId":"t1","title":"[HTTP]GET","traceLatencyMillis":2100}],"hasNextPage":false}`)
	}
	return false
}
