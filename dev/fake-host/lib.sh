# shellcheck shell=sh
# run.sh / stop.sh / retire.sh 共通の設定。直接実行しない。

here=$(cd "$(dirname "$0")" && pwd)
image=${MCRL_FAKE_HOST_IMAGE:-mcrl-fake-host:latest}

index=${1:-1}
case "$index" in
  ''|*[!0-9]*)
    echo "usage: $(basename "$0") [index]  (index は正の整数, 既定 1)" >&2
    exit 2
    ;;
esac

name="mcrl-fake-host-$index"
state_dir="$here/state/$name"

. "$here/../write-key.sh"

# Write 権限の API キー（../write-key.sh）を MACKEREL_APIKEY として export する（値は表示しない）
require_write_key() {
  load_write_key
  MACKEREL_APIKEY=$MACKEREL_APIKEY_WRITE
  export MACKEREL_APIKEY
}

container_exists() {
  container inspect "$name" >/dev/null 2>&1
}
