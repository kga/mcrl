# 05. コマンド体系

ステータス: **提案（レビュー待ち）**

コマンドの構成と、それぞれをなぜその形にしたかを書く。共通の約束（出力・名前解決・時刻）は [04-cli-ux.md](04-cli-ux.md)。

**フラグ・使用例・呼ぶ API・必要な権限は、コマンドのヘルプから生成した [reference/](reference/README.md) を正とする**（`mise run docs` で生成。[decisions.md](decisions.md) D21）。
どの API エンドポイントをどのコマンドが使うかは [reference/endpoints.md](reference/endpoints.md)。全エンドポイントにコマンドがあることはテスト（`TestEndpointCoverage`）で確かめている。

## 全体図

```
mcrl
├── auth        login / logout / status / list / switch / token
├── config      path / clear-cache（get / set / list / edit は未実装）
├── org         get / ip-restriction {get,set}
├── service     list / create / delete
├── role        list / create / delete
├── host        list / get / create / update / edit / status / retire / role {set,add,remove} / monitor-status / open（open は未実装）
├── metric      get / latest / post / names / graph-def {create,delete}
├── metadata    list / get / set / delete
├── monitor     list / get / create / update / edit / delete / mute / unmute / template / open（open は未実装）
├── check       list / get / report / run
├── alert       list / get / close / memo / logs / open（open は未実装）
├── downtime    list / get / create / update / edit / delete
├── alert-group list / get / create / update / edit / delete
├── channel     list / get / create / update / edit / delete
├── notification-group  list / get / create / update / edit / delete
├── dashboard   list / get / create / update / edit / delete / open（open は未実装）
├── annotation  list / create / update / delete / wrap
├── user        list / remove
├── invitation  list / create / revoke
├── aws         list / get / create / update / edit / delete / external-id / excludable-metrics
├── log         search / tail / saved
├── trace       list / get
├── apm         http / db
├── export / diff / apply   （宣言的管理、07-declarative.md。未実装）
├── audit       忘れられた設定・古くなった設定の棚卸し（参照のみ）
├── api         生 API 呼び出し
├── mcp         MCP サーバー（AI エージェントにコマンドをツールとして公開）
├── open        Web コンソールを開く（未実装）
├── doctor      設定・接続・権限の診断
├── completion  bash / zsh / fish / powershell
└── version
```

## 形の決め方

- コマンドは `mcrl <リソース> <動詞>` の 2 段を基本にし、API のセクションとほぼ 1 対 1 に対応させる。3 段目は `host role`・`metric graph-def`・`org ip-restriction` のような明確な下位リソースだけ（[04-cli-ux.md](04-cli-ux.md)）
- 動詞は全リソースで揃える: `list` / `get` / `create` / `update` / `edit` / `delete`
  - `update` は「今の値にフラグや `-f` を重ねて送る」。全体置換の API（監視・ダッシュボードなど）でも、指定しなかった項目が消えないようにするため。丸ごと置き換えたいときは `--replace`
  - `edit` は今の値を YAML にして `$EDITOR` で開き、差分を見せてから送る。読み取り専用の項目（`id`・`createdAt` など）は隠す
  - `get` は単体取得 API がないリソース（ダウンタイム・通知チャンネル・通知グループ）でも、一覧から取り出して用意する。利用者が API の不揃いを意識しなくて済むように
- ID だけでなく名前でも指定できる（ホスト名、監視名、ダッシュボードのタイトルなど）。名前から ID を引くのは CLI の仕事（[04-cli-ux.md](04-cli-ux.md)）
- 更新系で API が `{"success": true}` しか返さないときは、取り直して更新後のリソースを出す（[decisions.md](decisions.md) D12）。取り直す API がないもの（グラフ定義）は API の応答をそのまま出す
- 消すと戻せない操作（削除・退役・IP 制限の変更など）は確認を求め、端末でなければ `--yes` がないと実行しない

## 認証・設定

- `auth login` は API キーを `GET /org` で検証してからキーチェーンに保存する。間違ったキーを保存したまま気づかない事故を防ぐ
- `auth token` は他のツールにキーを渡すためのもの。端末に出すときは警告する
- `config` は設定ファイルの場所の表示と補完キャッシュの削除だけ。設定ファイルは直接編集してもらう（`get` / `set` などは必要になったら作る）

## 組織

- `org ip-restriction set` は、許可する範囲を確認画面に示し、自分も締め出されうることを明示する。API からも Web コンソールからも入れなくなるのを防ぐ

## サービス・ロール

- `service delete` はロール・サービスメトリクス・監視・アノテーションが **連鎖削除される** ことを確認画面で明示する
- ロールは `<service>:<role>` の 1 引数で指定する。Web コンソールやロールのフルネームと同じ書き方
- `role list` は `--service` 必須。全サービスのロール名は `service list` に含まれる

## ホスト

- `host create` はエージェントなしでホストを登録する。メトリクスやチェック監視をスクリプトから投稿する用途のため
- `host status` / `host retire` は複数ホストを受け取り、API の一括エンドポイントに 30 台ずつ分けて送る。`retire` は戻せないので確認画面に対象ホスト名を並べる
- ロールの変更は `host update` ではなく `host role` にまとめた。`PUT /hosts/<id>` は送らなかったロールを外さないため（[api-notes.md](api-notes.md)）、置き換え（`set`）と差分（`add` / `remove`）を別のコマンドにして意図を明確にする
- `host monitor-status` は、ホストを対象にしている各監視の状態。監視名は `--expand` で付ける

## メトリクス

- `metric get` / `metric names` / `metric post` は `--host` と `--service` でホストメトリクスとサービスメトリクスを切り替える。API は別々だが、利用者にとっては同じ操作のため
- `metric post` は mackerel-agent のプラグイン形式（`name\tvalue\tepoch`）を stdin から受け付ける。既存のプラグインの出力をそのまま流し込めるように
  ```sh
  ./my-plugin.sh | mcrl metric post --host "$(hostname)" -
  ```
- **24 時間より古い値は API が黙って捨てる** ため、CLI 側で警告する
- `metric graph-def` はカスタムメトリクスのグラフ定義の作成・削除。読み出す API がないので `list` / `get` はない

## メタデータ

- 対象は `--host`、`--service`、`--role <svc:role>` のどれか 1 つ。API はパスが 3 通りあるが、操作は同じなので 1 つのコマンドにまとめた
- 値は任意の JSON（YAML でも可）。100KB を超えるものは送る前に拒否する

## 監視・チェック監視

- `monitor create` / `update` はファイル（`-f`）が基本。監視は種類ごとに項目が多く、フラグに落とすと覚えきれないため。雛形は `monitor template <type>` で出す
- `monitor mute` / `unmute` は今の設定を取得して `isMute` だけ変えて送る。全体置換の API で他の項目を壊さないため
- `check report` はチェック監視の結果を 1 件報告する。`check run` はコマンドを実行し、その終了コード（0=OK、1=WARNING、2=CRITICAL、その他=UNKNOWN）と出力を報告する。cron のジョブを監視にするため
  ```sh
  mcrl check run --host "$(hostname)" --name backup -- /usr/local/bin/backup.sh
  ```

## アラート

- `alert list` は既定で未解決のものだけ。障害対応で最初に見たいのはそれだから
- `alert close` は `-` で stdin から複数の ID を受け取る。`alert list --jq` の結果をまとめて閉じられるように

## ダウンタイム

- `downtime create` は `--duration` と `--recurrence weekly:Mon,Wed` のような短い書き方で作れるようにする。API の形式（開始時刻の epoch、分単位の長さ、曜日の配列）を手で組み立てなくて済むように
  ```sh
  mcrl downtime create --name "緊急メンテ" --duration 2h --service web
  ```

## アラートグループ・通知

- `channel create` は API で作れる種別（email / slack / webhook）だけ受け付け、それ以外は明示的なエラーにする。一覧には全種別が出るが、詳細が取れるのも上の 3 種別だけ
- `notification-group` にはチャンネルや監視を名前で渡せる。別名は `ng`

## ダッシュボード・アノテーション

- ダッシュボードの作成・更新では、ウィジェットの配置（24 列、重なり、サイズ上限）を送る前に検証する。API のエラーより分かりやすく示すため
- `annotation wrap` はコマンドの開始から終了までをアノテーションにする。デプロイなどをグラフ上に記録するため。失敗したらタイトルに結果を付記する
  ```sh
  mcrl annotation wrap --service web --title "deploy $(git rev-parse --short HEAD)" -- ./deploy.sh
  ```

## ユーザー・招待・AWS インテグレーション

- `user remove` は ID・メール・スクリーン名で指定できる
- AWS インテグレーションの `secretKey` は出力しない・`edit` に含めない

## ログ・トレース・APM

- `log search` / `trace list` は API が POST で受ける多数の検索条件をフラグにする。`--where 'res.status>=500'` のような条件式は型を値から推論する
  ```sh
  mcrl log search --service web --since 1h --where 'res.status>=500' --severity error+
  ```
- `log tail` は新しいログを JSONL で出し続ける。API の 1 req/s の制限を守ってポーリングする
- 30 日を超える期間や APM の 30 分上限は、API に送る前にエラーにする

## 棚卸し（audit）

`audit` は、放っておくと腐る設定を一覧にする。何も変更しない（[decisions.md](decisions.md) D22）。
定期的に（cron や CI で）実行して、忘れられたミュートや使われていない通知先に気づけるようにする。

- 出力は「指摘」の配列。各要素は `{"kind", "severity", "resource": {"type", "id", "name"}, "message", "details"}`。`-o jsonl` と `--jq` で絞れる
- 重要度は `warning`（通知が届かない・監視が効いていないなど、放置すると困るもの）と `info`（片付ければすっきりするもの）の 2 段
- `--check <kind>` で一部だけ実行する（繰り返し・カンマ区切り可）。選んだチェックに要る一覧だけを取る
- `--older-than`（既定 `30d`）はホストのチェックのしきい値
- `--fail-on warning|info` で、その重要度以上の指摘があれば **終了コード 8** にする。出力はそのまま出る。既定では指摘があっても 0
- 各一覧は 1 回だけ取り、チェックの間で使い回す。全チェックで約 10 リクエスト（ホスト 100 台ごとに `GET /tsdb/latest` が 1 回、ホストのないサービスごとに `metric-names` が 1 回、退役済みホストのアラートがあればホストごとに 1 回増える）

| kind | 重要度 | 判定基準 |
|---|---|---|
| `muted-monitor` | warning | 監視の `isMute` が `true` |
| `ended-downtime` | info | もう有効にならないダウンタイム。繰り返しなしは `start + duration` が現在以前、繰り返しありは `recurrence.until + duration` が現在以前（最後の回の終わりの上限）。`until` のない繰り返しは対象外。ちょうど終了時刻のものは終わったとみなす（`downtime list --active` と同じ半開区間） |
| `dangling-scope` | warning | 監視の `scopes`・`excludeScopes`・サービス監視の `service`、ダウンタイムの `serviceScopes`・`roleScopes`（と各 `Exclude`）・`monitorScopes`（と `monitorExcludeScopes`）が、存在しないサービス・ロール・監視を指している |
| `host-inactive` | info | `standby`・`maintenance`・`poweroff` のホストで、最後のメトリクス（なければ登録日時 `createdAt`）が `--older-than` より前 |
| `host-stale-metrics` | warning | `working` のホストで、最後のメトリクスが `--older-than` より前 |
| `host-without-role` | info | ロールが 1 つもないホスト（サービス・ロール単位の監視の対象にならない） |
| `alert-on-retired-host` | warning | 未解決のアラートのうち、ホストが一覧になく、`GET /hosts/<id>` で `isRetired: true`（または取得できない）もの |
| `unused-service` | info | どのホストのロールにも現れず、サービスメトリクスもないサービス |
| `unused-role` | info | どのホストにも付いていないロール |
| `unused-channel` | info | どの通知グループの `childChannelIds` にも入っていない通知チャンネル |
| `empty-notification-group` | warning | `childChannelIds` と `childNotificationGroupIds` がどちらも空の通知グループ（デフォルトのグループも含む） |
| `user-without-mfa` | warning | `isMFAEnabled` が `false` のユーザー |
| `expired-invitation` | info | `expiresAt` が現在以前の招待 |

- 「最後のメトリクス」は `GET /tsdb/latest` で `loadavg5`・`cpu.user.percentage`・`memory.used`（mackerel-agent が Linux・Windows で送るもの）の最新時刻の最大値。API だけで登録したホストなど、どれも持たないホストは `host-stale-metrics` の対象外で、`host-inactive` では登録日時で判断する
- ホストの API にはステータスを変えた時刻がないので、「長く poweroff のまま」はメトリクスの途絶で代用している
- 退役したホストに残ったチェック監視は判定できない。`GET /monitoring/checks` の各要素は `id` と `name` だけで、ホストとの対応がないため（[api-notes.md](api-notes.md#棚卸しに使った事実実測-2026-11-07)）。代わりに、退役したホストの未解決アラート（チェック監視のものを含む）を `alert-on-retired-host` で出す

```sh
# 週 1 回の棚卸し。warning があれば終了コード 8
mcrl audit --fail-on warning -o jsonl > audit.jsonl
```

## 横断機能

- `api <path>` は生 API。コマンドのない操作や、まだ対応していない新しい API もこれで呼べる。`--paginate` で `nextId` をたどる
  ```sh
  mcrl api /hosts --jq '.hosts[].name'
  ```
- `doctor` は設定ファイル・キーの保存場所・疎通・キーの有効性・時計のずれ・プロキシ設定を診断する。問い合わせの前に自分で切り分けられるように
- `completion` のホスト名・監視名などは API から動的に補完する（結果は短時間キャッシュ）

## MCP サーバー

- `mcp` は stdio で MCP サーバーとして動き、mcrl のコマンドを AI エージェントにツールとして公開する。ツール呼び出しはコマンドの実行に変換するので、名前解決・検証・エラーが CLI と同じになる（[decisions.md](decisions.md) D20）
- 既定は参照系だけ。`--allow-write` で作成・更新系、`--allow-destructive` で CLI が確認を求めるコマンド（削除・退役など）も公開する。MCP では端末で確認を取れないため、起動時の明示的な許可にする
- ツールの入力スキーマと説明文は、フラグ・`Use`・`Long` から作る。`--help` と同じ定義なので食い違わない
- `--file` は `body` にして stdin で渡す。ファイルはサーバー側のマシンにあるため
- エディタを開く（`edit`）・ローカルのコマンドを実行する（`check run`、`annotation wrap`）・終わらない（`log tail`）・認証情報を扱う（`auth`）コマンドは公開しない。`api` は GET だけ
- どのコマンドをどの区分で公開するかは `internal/cli/mcp.go` の表で決め、全コマンドが表か除外リストにあることをテストで確かめる

```sh
claude mcp add mcrl -- mcrl mcp
```

## 拡張（未実装）

`PATH` 上の `mcrl-<name>` を `mcrl <name>` として実行できるようにする。
子プロセスには解決済みの `MACKEREL_APIKEY` と `MCRL_API_BASE` を渡し、プロファイル解決を再実装しなくて済むようにする。

## API の制約・不揃い

API の振る舞いのうち、コマンドの設計に影響したもの。実測の詳細は [api-notes.md](api-notes.md)。

- メトリクス投稿: 24 時間より古いタイムスタンプは **200 を返しつつ黙って捨てられる**
- チェック監視レポート: `occurredAt` が 6 時間以上未来、または同一 host/name で投稿済みのものは無視
- サービス・ロール名: `^[A-Za-z0-9][A-Za-z0-9_-]+$`、2–63 文字
- メトリクス名: `[a-zA-Z0-9._-]+`
- グラフ定義名: `^custom(\.([-a-zA-Z0-9_]+|[*#]))+$`
- メタデータ namespace: `[-a-zA-Z0-9_]+`、`mackerel` で始まる名前は予約
- メタデータ数上限: host 50 / service 50 / role 10、値は任意の JSON 100KB まで
- サービス削除はロール・サービスメトリクス・監視設定・グラフアノテーションを **連鎖削除**
- 退役済みホストへのメタデータ書き込みは 400、退役 1 週間超のホストのメタデータ読み取りも 400
- チェック監視は `/api/v0/monitoring/checks`（`/monitors` 配下ではない）
- 招待の取り消しは `POST /invitations/revoke`（DELETE ではない）
- グラフ定義の作成は `POST /graph-defs/create`、削除は `DELETE /graph-defs`（body に name）
- Logs / Traces の一覧は検索条件が多いため POST
- AWS インテグレーション一覧のキーは `aws_integrations`（スネークケース）
- `GET /hosts.json` は `GET /hosts` と同一
- 監視・通知チャンネル・通知グループ・ユーザー・ホストの ID は **11 文字の英数字**（実測）。名前解決では「11 文字の英数字なら ID とみなし、それ以外は名前」とし、`name:` 接頭辞で強制できる
