#!/bin/sh
# fake host を Mackerel から退役させ、ローカル状態も消す。 usage: retire.sh [index]
# 手順: コンテナ停止・削除 → 同じ state を載せた使い捨てコンテナで
#       `mackerel-agent retire -force` → 成功したら state ディレクトリを削除。
set -eu
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

require_write_key

if [ ! -s "$state_dir/id" ]; then
  echo "no host id at $state_dir/id; nothing to retire" >&2
  exit 1
fi

# 稼働中のエージェントが退役後に投稿し続けないよう、先に止める
if container_exists; then
  container stop "$name" >/dev/null 2>&1 || true
  container delete "$name" >/dev/null
  echo "stopped and removed: $name"
fi

echo "retiring host $(cat "$state_dir/id") ..."
container run \
  --rm \
  --env MACKEREL_APIKEY \
  --volume "$state_dir:/var/lib/mackerel-agent" \
  "$image" retire -force

rm -rf "$state_dir"
echo "retired and removed local state: $state_dir"
