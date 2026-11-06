#!/bin/sh
# 検証用組織にテスト用のトレース 3 本とログ 3 件を OTLP/HTTP (JSON) で送る。
# サービス名は mcrl-otel。リポジトリ直下で `mise exec -- dev/otlp/send.sh` として実行する。
# キーは MACKEREL_APIKEY_WRITE（Write 権限）を使い、表示しない。
set -eu
here=$(cd "$(dirname "$0")" && pwd)
if [ -z "${MACKEREL_APIKEY_WRITE:-}" ]; then
  echo "error: MACKEREL_APIKEY_WRITE is not set" >&2
  exit 1
fi
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
OUT=$out python3 -I "$here/generate.py" > "$out/trace-ids.txt"
for kind in traces:https://otlp-vaxila.mackerelio.com/v1/traces logs:https://otlp.mackerelio.com:4318/v1/logs; do
  name=${kind%%:*}
  url=${kind#*:}
  printf '%s: ' "$name"
  curl -sS -o /dev/null -w 'HTTP %{http_code}\n' -X POST "$url" \
    -H "Mackerel-Api-Key: $MACKEREL_APIKEY_WRITE" -H 'Accept: */*' -H 'Content-Type: application/json' \
    --data-binary "@$out/$name.json"
done
echo "trace IDs:"
cat "$out/trace-ids.txt"
