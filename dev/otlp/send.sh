#!/bin/sh
# 検証用組織にテスト用のトレース 3 本とログ 3 件を OTLP/HTTP (JSON) で送る。
# サービス名は mcrl-otel。リポジトリ直下で `dev/otlp/send.sh` として実行する。
# キーは Write 権限のもの（MACKEREL_APIKEY_WRITE、なければ mcrl のプロファイル。../write-key.sh）を使い、表示しない。
set -eu
here=$(cd "$(dirname "$0")" && pwd)
. "$here/../write-key.sh"
load_write_key
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
