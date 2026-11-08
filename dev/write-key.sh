# shellcheck shell=sh
# dev/ のスクリプト共通。直接実行しない。
#
# load_write_key: Write 権限の API キーを MACKEREL_APIKEY_WRITE に入れる（値は表示しない）。
# - MACKEREL_APIKEY_WRITE がすでにあれば、それを使う
# - なければ mcrl のプロファイル（既定 mcrl-dev-rw。MCRL_DEV_WRITE_PROFILE で変える）から
#   `mcrl auth token` で取り出す。キーの保管場所（キーチェーン、api_key_command など）は mcrl に任せる
load_write_key() {
  if [ -n "${MACKEREL_APIKEY_WRITE:-}" ]; then
    return 0
  fi
  profile=${MCRL_DEV_WRITE_PROFILE:-mcrl-dev-rw}
  if ! command -v mcrl >/dev/null 2>&1; then
    echo "error: MACKEREL_APIKEY_WRITE is not set and mcrl is not in PATH." >&2
    exit 1
  fi
  # 環境変数の MACKEREL_APIKEY はプロファイルより優先されるので、外してから取り出す
  if ! MACKEREL_APIKEY_WRITE=$(env -u MACKEREL_APIKEY mcrl --profile "$profile" auth token); then
    echo "error: could not get the API key of mcrl profile \"$profile\"." >&2
    echo "       Set up a profile with a Write API key of the test organization" >&2
    echo "       (or set MCRL_DEV_WRITE_PROFILE, or MACKEREL_APIKEY_WRITE)." >&2
    exit 1
  fi
}
