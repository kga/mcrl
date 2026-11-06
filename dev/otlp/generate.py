# OTLP/HTTP JSON のテスト用トレースとログを作る（送信は curl で行う）
import json, os, time, secrets
now = time.time_ns()
res = {"attributes": [
    {"key": "service.name", "value": {"stringValue": "mcrl-otel"}},
    {"key": "deployment.environment.name", "value": {"stringValue": "test"}},
    {"key": "service.version", "value": {"stringValue": "0.0.1"}},
]}
traces, logs = [], []
ids = []
for i, (route, status, ms) in enumerate([("/users/{id}", 200, 120), ("/orders", 500, 900), ("/health", 200, 3)]):
    tid, root = secrets.token_hex(16), secrets.token_hex(8)
    ids.append(tid)
    start = now - (60 - i) * 1_000_000_000
    end = start + ms * 1_000_000
    spans = [{
        "traceId": tid, "spanId": root, "name": f"GET {route}", "kind": 2,
        "startTimeUnixNano": str(start), "endTimeUnixNano": str(end),
        "attributes": [
            {"key": "http.request.method", "value": {"stringValue": "GET"}},
            {"key": "http.route", "value": {"stringValue": route}},
            {"key": "http.response.status_code", "value": {"intValue": str(status)}},
        ],
        "status": {"code": 2 if status >= 500 else 1},
    }, {
        "traceId": tid, "spanId": secrets.token_hex(8), "parentSpanId": root, "name": "SELECT users", "kind": 3,
        "startTimeUnixNano": str(start + 1_000_000), "endTimeUnixNano": str(start + ms * 500_000),
        "attributes": [
            {"key": "db.system.name", "value": {"stringValue": "mysql"}},
            {"key": "db.query.text", "value": {"stringValue": "SELECT * FROM users WHERE id = ?"}},
        ],
    }]
    traces.extend(spans)
    logs.append({
        "timeUnixNano": str(start), "observedTimeUnixNano": str(start),
        "severityNumber": 17 if status >= 500 else 9, "severityText": "ERROR" if status >= 500 else "INFO",
        "body": {"stringValue": json.dumps({"req": {"method": "GET", "path": route}, "res": {"status": status}, "msg": f"mcrl test {route}"})},
        "traceId": tid, "spanId": root,
    })
scope = {"name": "mcrl-test"}
json.dump({"resourceSpans": [{"resource": res, "scopeSpans": [{"scope": scope, "spans": traces}]}]}, open(os.environ["OUT"] + "/traces.json", "w"))
json.dump({"resourceLogs": [{"resource": res, "scopeLogs": [{"scope": scope, "logRecords": logs}]}]}, open(os.environ["OUT"] + "/logs.json", "w"))
print("\n".join(ids))
