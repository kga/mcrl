#!/bin/sh
# mcrl fake host のエントリポイント。
#
# - 環境変数 MACKEREL_APIKEY があれば、それを apikey として
#   /etc/mackerel-agent/mackerel-agent.conf を（コンテナ内の書き込み層に）生成する。
#   キーは標準出力・ログには出さない。
# - 引数なし: mackerel-agent をフォアグラウンドで起動（コンテナのメインプロセス）。
#   この場合 MACKEREL_APIKEY は必須。
# - 引数あり: mackerel-agent のサブコマンドとして実行（例: version, retire -force, configtest）。
set -eu

conf=/etc/mackerel-agent/mackerel-agent.conf
base=/etc/mackerel-agent/mackerel-agent.conf.base

if [ -n "${MACKEREL_APIKEY:-}" ]; then
  # TOML の基本文字列に安全に入る文字だけを許可する
  case "$MACKEREL_APIKEY" in
    *[!A-Za-z0-9+/=_.-]*)
      echo "entrypoint: MACKEREL_APIKEY contains unexpected characters" >&2
      exit 1
      ;;
  esac
  umask 077
  {
    printf 'apikey = "%s"\n' "$MACKEREL_APIKEY"
    cat "$base"
  } > "$conf"
  umask 022
  # エージェントやプラグインの環境にキーを残さない
  unset MACKEREL_APIKEY
fi

if [ "$#" -eq 0 ]; then
  if [ ! -s "$conf" ]; then
    echo "entrypoint: MACKEREL_APIKEY is not set; refusing to start mackerel-agent" >&2
    exit 1
  fi
  mkdir -p -m 777 /var/tmp/mackerel-agent
  export MACKEREL_PLUGIN_WORKDIR=/var/tmp/mackerel-agent
  exec /usr/bin/mackerel-agent -conf "$conf"
fi

exec /usr/bin/mackerel-agent "$@"
