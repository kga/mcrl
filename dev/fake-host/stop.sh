#!/bin/sh
# fake host コンテナを停止・削除する（Mackerel 上のホストは退役しない）。 usage: stop.sh [index]
# ホスト ID は state/ に残るので、run.sh で再起動すると同じホストとして復帰する。
set -eu
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

if ! container_exists; then
  echo "container '$name' does not exist"
  exit 0
fi

# 停止時、エージェントはホストステータスを poweroff にする（on_stop 設定）
container stop "$name" >/dev/null 2>&1 || true
container delete "$name" >/dev/null
echo "stopped and removed: $name"
