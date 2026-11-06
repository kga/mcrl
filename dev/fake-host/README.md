# fake host（開発用）

mcrl の開発で Mackerel API を試すための、使い捨てのテスト用ホストです。
Debian 13 のコンテナに Mackerel 公式 apt リポジトリから `mackerel-agent` を入れて動かし、
テスト用オーガニゼーションにホストとして登録します。

## 前提

- Apple の `container` CLI（`container system start` 済み）
- `mise`。リポジトリ直下の `mise.local.toml`（gitignore 済み）に Write 権限の API キーを
  `MACKEREL_APIKEY_WRITE` として設定しておく

## 使い方（リポジトリ直下で実行）

```sh
dev/fake-host/build.sh                       # イメージ mcrl-fake-host:latest をビルド（キー不要）
mise exec -- dev/fake-host/run.sh [index]    # mcrl-fake-host-<index> を起動（index の既定は 1）
container logs -f mcrl-fake-host-1           # ログを見る
dev/fake-host/stop.sh [index]                # 停止・削除（ホストは退役させない。ID は残る）
mise exec -- dev/fake-host/retire.sh [index] # 停止 → Mackerel から退役 → ローカル状態を削除
```

- コンテナ名がそのまま Mackerel 上のホスト名になります（例: `mcrl-fake-host-1`）。
- index を変えると、別ホストとして複数台を立てられます。
- 使い終わったら必ず `retire.sh` で退役させ、不要なホストを残さないでください。
- Mac の再起動などでコンテナが止まった場合は、`dev/fake-host/stop.sh [index]` で消してから
  `mise exec -- dev/fake-host/run.sh [index]` で起動し直すと、同じホストとして復帰します。
  止まっている間、Mackerel 上のホストステータスは `poweroff` になります。

## API キーの渡し方

1. `run.sh` / `retire.sh` は呼び出し元の `MACKEREL_APIKEY_WRITE` を `MACKEREL_APIKEY` として export し、
   `container run --env MACKEREL_APIKEY`（値なし＝環境から引き継ぐ）でコンテナに渡します。
   キーがコマンドラインに載ることはありません。
2. mackerel-agent 自体は環境変数からキーを読みません（`-apikey` フラグは非推奨）。
   そこで `entrypoint.sh` が起動時に、コンテナ内の `/etc/mackerel-agent/mackerel-agent.conf`
   （パーミッション 0600）へ `apikey = "..."` を書き込みます。その後、環境変数からは消してから
   エージェントを起動します。
3. イメージにはキーは含まれません。キーが残るのは実行中のコンテナの書き込み層だけで、
   コンテナを削除すると消えます。

注意:

- `container inspect <name>` を実行すると、環境変数としてキーが表示されます。
- `mise exec` は `mise.local.toml` の値を注入します。そのため、シェルで変数を unset していても
  キーは渡ります。キーを使わずに試したいときは、`mise exec` を付けずに実行してください。

## 状態の保存先

ホスト ID は `dev/fake-host/state/mcrl-fake-host-<index>/id` に保存されます（gitignore 済み）。
このディレクトリをコンテナの `/var/lib/mackerel-agent` にマウントしているので、
コンテナを作り直しても同じホストとして復帰します。このディレクトリを消してから起動すると、
新しいホストとして登録されます。消す前に `retire.sh` を実行してください。
