#!/bin/sh
# fake host コンテナを起動する。 usage: run.sh [index]
# Write 権限の API キー（MACKEREL_APIKEY_WRITE、なければ mcrl のプロファイル。../write-key.sh）を、
# コンテナには MACKEREL_APIKEY として（値をコマンドラインに載せずに）渡す。
set -eu
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

require_write_key

if container_exists; then
  echo "error: container '$name' already exists. 先に stop.sh $index を実行してください。" >&2
  exit 1
fi

mkdir -p "$state_dir"
if [ -s "$state_dir/id" ]; then
  echo "reusing host id from $state_dir/id"
else
  echo "no host id yet; the agent will register a new host"
fi

# -e MACKEREL_APIKEY（値なし）は呼び出し元の環境から値を引き継ぐ。
# コンテナ名 = ホスト名 = Mackerel 上のホスト名になる。
container run \
  --detach \
  --init \
  --name "$name" \
  --label mcrl.fake-host=1 \
  --env MACKEREL_APIKEY \
  --volume "$state_dir:/var/lib/mackerel-agent" \
  "$image" >/dev/null

echo "started: $name (state: $state_dir)"
echo "logs:    container logs -f $name"
