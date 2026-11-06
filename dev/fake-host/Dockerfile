# mcrl 開発用の使い捨て「フェイクホスト」。
# mackerel-agent を Mackerel 公式 apt リポジトリから入れる。
# リポジトリ設定は公式手順 https://mackerel.io/ja/docs/entry/howto/install-agent/deb で案内されている
# セットアップスクリプト https://mackerel.io/file/script/setup-all-apt-v2.sh のうち
# リポジトリ登録・パッケージ導入部分だけを再現したもの（API キーを使う init / systemctl 部分は使わない）。
#
# API キーはビルド時には一切扱わない。実行時に環境変数 MACKEREL_APIKEY で渡し、
# entrypoint.sh がコンテナ内の /etc/mackerel-agent/mackerel-agent.conf を生成する。
FROM debian:13-slim

ARG DEBIAN_FRONTEND=noninteractive

RUN set -eu; \
    apt-get update -qq; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg; \
    mkdir -p /etc/apt/keyrings; \
    curl -LfsS https://mackerel.io/file/cert/GPG-KEY-mackerel-v2 \
      | gpg --dearmor -o /etc/apt/keyrings/mackerel-v2-archive-keyring.gpg; \
    echo "deb [arch=amd64,arm64 signed-by=/etc/apt/keyrings/mackerel-v2-archive-keyring.gpg] http://apt.mackerel.io/v2/ mackerel contrib" \
      > /etc/apt/sources.list.d/mackerel.list; \
    apt-get update -qq; \
    apt-get install -y --no-install-recommends mackerel-agent; \
    apt-get purge -y --auto-remove gnupg; \
    rm -rf /var/lib/apt/lists/*; \
    rm -f /etc/mackerel-agent/mackerel-agent.conf

# apikey 以外の設定（キーを含まない）。entrypoint.sh が先頭に apikey 行を足して本番 conf を作る。
COPY mackerel-agent.conf.base /etc/mackerel-agent/mackerel-agent.conf.base
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod 0755 /usr/local/bin/entrypoint.sh

# ホスト ID（/var/lib/mackerel-agent/id）はここに保存される。run.sh がホスト側ディレクトリをマウントする。
VOLUME ["/var/lib/mackerel-agent"]

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
