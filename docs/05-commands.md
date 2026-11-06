# 05. コマンド体系

ステータス: **提案（レビュー待ち）**

全エンドポイントを覆うコマンドの一覧。共通の約束（出力・名前解決・時刻）は [04-cli-ux.md](04-cli-ux.md)。
表の「API」列が空のものは、CLI が複数の API を組み合わせて提供する機能。

## 全体図

```
mcrl
├── auth        login / logout / status / list / switch / token
├── config      get / set / list / edit / path
├── org         get / ip-restriction {get,set}
├── service     list / create / delete
├── role        list / create / delete
├── host        list / get / create / update / edit / status / retire / role / monitor-status / open
├── metric      get / latest / post / names / graph-def {create,delete}
├── metadata    list / get / set / delete
├── monitor     list / get / create / update / edit / delete / mute / unmute / template / open
├── check       list / get / report / run
├── alert       list / get / close / memo / logs / open
├── downtime    list / get / create / update / edit / delete
├── alert-group list / get / create / update / edit / delete
├── channel     list / get / create / update / delete
├── notification-group  list / get / create / update / edit / delete
├── dashboard   list / get / create / update / edit / delete / open
├── annotation  list / create / update / delete / wrap
├── user        list / remove
├── invitation  list / create / revoke
├── aws         list / get / create / update / edit / delete / external-id / excludable-metrics
├── log         search / tail / saved
├── trace       list / get
├── apm         http / db
├── export / diff / apply   （宣言的管理、07-declarative.md）
├── api         生 API 呼び出し
├── open        Web コンソールを開く
├── doctor      設定・接続・権限の診断
├── completion  bash / zsh / fish / powershell
└── version
```

## 認証・設定

| コマンド | 説明 | API |
|---|---|---|
| `auth login [--profile p]` | API キーを対話入力（または `--with-token` で stdin）し、`GET /org` で検証してキーチェーンに保存 | `GET /org` |
| `auth logout` | キーチェーンから削除 | |
| `auth status` | 現在のプロファイル・組織名・キーの保存場所・ベース URL を表示 | `GET /org` |
| `auth list` / `auth switch <p>` | プロファイル一覧・既定の切替 | |
| `auth token` | API キーを stdout に出す（他ツールに渡す用。TTY では警告） | |
| `config get/set/list/edit/path` | 設定ファイルの操作。`path` は読む設定ファイルの場所を全て表示 | |

## 組織

| コマンド | 説明 | API |
|---|---|---|
| `org get` | 組織名・表示名 | `GET /org` |
| `org ip-restriction get --target web\|api` | IP 制限の取得 | `GET /settings/ip-restriction/{web,api}` |
| `org ip-restriction set --target web\|api --cidr ...` | IP 制限の設定。`--clear` で解除。**自分を締め出す設定は警告**（現在の送信元 IP が範囲外になる場合） | `POST /settings/ip-restriction/{web,api}` |

## サービス・ロール

| コマンド | 説明 | API |
|---|---|---|
| `service list` | 一覧 | `GET /services` |
| `service create <name> [--memo]` | 作成 | `POST /services` |
| `service delete <name>` | 削除。**ロール・サービスメトリクス・監視・アノテーションが連鎖削除される** ことを確認画面で明示 | `DELETE /services/<name>` |
| `role list --service s` | 一覧（`--service` は必須。全サービスのロール名は `service list` に含まれる） | `GET /services/<s>/roles` |
| `role create <svc>:<role> [--memo]` | 作成 | `POST /services/<s>/roles` |
| `role delete <svc>:<role>` | 削除 | `DELETE /services/<s>/roles/<r>` |

## ホスト

| コマンド | 説明 | API |
|---|---|---|
| `host list` | `--service`、`--role`（複数可）、`--name`、`--status`（複数可、`all` で全て）、`--custom-identifier` | `GET /hosts` |
| `host get <host>` | 詳細 | `GET /hosts/<id>`、`GET /hosts-by-custom-identifier/<cid>` |
| `host create` | `--name`、`--display-name`、`--role`、`--memo`、`--custom-identifier`、または `-f` | `POST /hosts` |
| `host update <host>` / `host edit <host>` | 更新 | `PUT /hosts/<id>` |
| `host status <status> <host>...` | ステータス変更。31 台以上は 30 台ずつに分けて送る | `POST /hosts/<id>/status`、`POST /hosts/bulk-update-statuses` |
| `host retire <host>...` | 退役。30 台ずつに分割。確認画面に対象ホスト名を列挙 | `POST /hosts/<id>/retire`、`POST /hosts/bulk-retire` |
| `host role set <host> <svc:role>...` | ロールを置き換え | `PUT /hosts/<id>/role-fullnames` |
| `host role add/remove <host> <svc:role>...` | 現在のロールを取得して追加・削除 | `GET /hosts/<id>` + `PUT .../role-fullnames` |
| `host monitor-status <host>` | 各監視のステータス。`--expand` で監視名を付加 | `GET /hosts/<id>/monitored-statuses` + `GET /monitors` |
| `host open <host>` | Web コンソールのホスト画面を開く | |

## メトリクス

| コマンド | 説明 | API |
|---|---|---|
| `metric get --host h --name n [--since --until]` | 時系列を取得 | `GET /hosts/<id>/metrics` |
| `metric get --service s --name n ...` | サービスメトリクス | `GET /services/<s>/metrics` |
| `metric latest --host h... [--name n...]` | 最新値 | `GET /tsdb/latest` |
| `metric post --host h name=value[@time]...` | 投稿。stdin からは mackerel-agent のプラグイン形式（`name\tvalue\tepoch`）か JSON を受け付ける | `POST /tsdb` |
| `metric post --service s ...` | サービスメトリクス投稿。**24 時間より古い値は API が黙って捨てるため、CLI 側で警告** | `POST /services/<s>/tsdb` |
| `metric names --host h \| --service s` | メトリクス名一覧 | `GET /hosts/<id>/metric-names`、`GET /services/<s>/metric-names` |
| `metric graph-def create -f` | グラフ定義の投稿 | `POST /graph-defs/create` |
| `metric graph-def delete <name>` / `--service s <name>` | グラフ定義の削除 | `DELETE /graph-defs`、`DELETE /services/<s>/graph-defs/<name>` |

```sh
# cron から: 任意のコマンドの出力をそのままホストメトリクスとして投稿
./my-plugin.sh | mcrl metric post --host "$(hostname)" -
```

## メタデータ

対象は `--host`、`--service`、`--role <svc:role>` のいずれか 1 つで指定する。

| コマンド | 説明 | API |
|---|---|---|
| `metadata list --host h` | namespace の一覧 | `GET .../metadata` |
| `metadata get --host h <ns>` | 値（任意の JSON）をそのまま出す。`Last-Modified` は `--expand` で付加 | `GET .../metadata/<ns>` |
| `metadata set --host h <ns> -f value.json` | 登録・更新。100KB 超はクライアント側で拒否 | `PUT .../metadata/<ns>` |
| `metadata delete --host h <ns>` | 削除 | `DELETE .../metadata/<ns>` |

## 監視

| コマンド | 説明 | API |
|---|---|---|
| `monitor list [--type t] [--name 部分一致]` | 一覧。フィルタはクライアント側 | `GET /monitors` |
| `monitor get <monitor>` | 詳細 | `GET /monitors/<id>` |
| `monitor create -f` | 作成 | `POST /monitors` |
| `monitor update <monitor> -f` / `edit` | 更新（全体置換 API なので、`-f` の内容は現在値にマージしてから送る。`--replace` で丸ごと置換） | `GET` + `PUT /monitors/<id>` |
| `monitor delete <monitor>` | 削除 | `DELETE /monitors/<id>` |
| `monitor mute/unmute <monitor>...` | ミュート切替（現在値を取得し `isMute` だけ変えて PUT） | `GET` + `PUT /monitors/<id>` |
| `monitor template <type>` | 種別ごとの雛形 YAML（`host`、`connectivity`、`service`、`external`、`expression`、`anomalyDetection`、`query`） | |
| `monitor open <monitor>` | Web コンソールで開く | |

## チェック監視

| コマンド | 説明 | API |
|---|---|---|
| `check list` / `check get <id>` | チェック監視の一覧・取得 | `GET /monitoring/checks[/<id>]` |
| `check report --host h --name n --status s --message m` | 1 件報告。stdin からの JSON 配列は 100 件ずつに分割 | `POST /monitoring/checks/report` |
| `check run --host h --name n [--interval 10m] -- <command>` | コマンドを実行し、終了コード（0=OK、1=WARNING、2=CRITICAL、その他=UNKNOWN）と出力を報告 | `POST /monitoring/checks/report` |

```sh
# cron: バックアップの成否をチェック監視として報告
mcrl check run --host "$(hostname)" --name backup -- /usr/local/bin/backup.sh
```

## アラート

| コマンド | 説明 | API |
|---|---|---|
| `alert list` | 既定は未解決のみ。`--all`（解決済み含む）、`--status`、`--type`、`--monitor`、`--host`、`--limit`（既定 100、`0` で無制限） | `GET /alerts` |
| `alert get <id>` | 詳細。`--expand` で監視名・ホスト名を付加 | `GET /alerts/<id>` |
| `alert close <id>... --reason r` | クローズ。`-` で stdin から複数 | `POST /alerts/<id>/close` |
| `alert memo <id> <text>` | メモ更新（80KB 超はクライアント側で拒否） | `PUT /alerts/<id>` |
| `alert logs <id>` | アラートの履歴 | `GET /alerts/<id>/logs` |
| `alert open <id>` | Web コンソールで開く | |

## ダウンタイム

| コマンド | 説明 | API |
|---|---|---|
| `downtime list [--active]` | 一覧。`--active` で現在有効なもの（クライアント側で計算） | `GET /downtimes` |
| `downtime get <downtime>` | 一覧から抽出 | `GET /downtimes` |
| `downtime create` | `--name`、`--start`（既定 `now`）、`--duration`、`--recurrence weekly:Mon,Wed`、`--interval`、`--until`、`--service`、`--role`、`--monitor`、各 `--exclude-*`。または `-f` | `POST /downtimes` |
| `downtime update/edit/delete <downtime>` | | `PUT`/`DELETE /downtimes/<id>` |

```sh
# 今から 2 時間、web サービスの監視を止める
mcrl downtime create --name "緊急メンテ" --duration 2h --service web
```

## アラートグループ・通知

| コマンド | 説明 | API |
|---|---|---|
| `alert-group list/get/create/update/edit/delete` | アラートグループ設定 | `/alert-group-settings` |
| `channel list` | 全種別を一覧（詳細が取れるのは email / slack / webhook のみ） | `GET /channels` |
| `channel get <channel>` | 一覧から抽出 | `GET /channels` |
| `channel create email\|slack\|webhook` | 種別ごとのフラグ。**API で作れない種別は指定時に明示的なエラー** | `POST /channels` |
| `channel update/delete <channel>` | | `PUT`/`DELETE /channels/<id>` |
| `notification-group list/get/create/update/edit/delete` | 通知グループ（別名 `ng`） | `/notification-groups` |

## ダッシュボード・アノテーション

| コマンド | 説明 | API |
|---|---|---|
| `dashboard list/get/create/update/edit/delete` | `get` は ID・URL パス・タイトルで指定可。作成・更新時にウィジェットの配置（24 列、重なり、サイズ上限）をクライアント側で検証 | `/dashboards` |
| `dashboard open <dashboard>` | Web コンソールで開く | |
| `annotation list --service s [--since --until]` | 一覧（既定は直近 7 日） | `GET /graph-annotations` |
| `annotation create --service s --title t [--from --to --role --description]` | 作成（`--from`/`--to` 省略時は現在時刻） | `POST /graph-annotations` |
| `annotation update/delete <id>` | | `PUT`/`DELETE /graph-annotations/<id>` |
| `annotation wrap --service s --title t -- <command>` | コマンドの開始〜終了をアノテーションにする。失敗時はタイトルに結果を付記 | `POST` + `PUT /graph-annotations` |

```sh
# デプロイをグラフ上に記録
mcrl annotation wrap --service web --title "deploy $(git rev-parse --short HEAD)" -- ./deploy.sh
```

## ユーザー・招待

| コマンド | 説明 | API |
|---|---|---|
| `user list` | 一覧（権限・MFA・認証方式） | `GET /users` |
| `user remove <user>` | 組織から外す。ID・メール・スクリーン名で指定 | `DELETE /users/<id>` |
| `invitation list/create/revoke` | `create <email> --authority viewer` | `/invitations`、`POST /invitations/revoke` |

## AWS インテグレーション

| コマンド | 説明 | API |
|---|---|---|
| `aws list/get/create/update/edit/delete` | `secretKey` は出力しない・`edit` に含めない | `/aws-integrations` |
| `aws external-id` | 外部 ID の発行 | `POST /aws-integrations-external-id` |
| `aws excludable-metrics [--service EC2]` | 除外可能なメトリクス | `GET /aws-integrations-excludable-metrics` |

## ログ・トレース・APM

| コマンド | 説明 | API |
|---|---|---|
| `log search --service s` | `--since`（既定 15m）、`--until`、`--keyword`（複数）、`--severity`（`warn+` で WARN 以上）、`--trace-id`、`--where 'req.status>=500'`（body 属性の条件。型は値から推論、`:int` などで明示）、`--saved <名前>`（保存された検索を使う）、`--limit`、`--asc` | `POST /logs` |
| `log tail --service s ...` | 新しいログを JSONL で出し続ける（1 req/s を守りつつポーリング） | `POST /logs` |
| `log saved` | 保存された検索の一覧 | `GET /saved-log-searches` |
| `trace list --service s` | `--since`、`--env`、`--span-name`、`--status error`、`--min-latency 500ms`、`--attr`、`--sort latency` | `POST /traces` |
| `trace get <traceId>` | span の一覧 | `GET /traces/<id>` |
| `apm http --service s` | HTTP サーバー統計（既定 直近 30 分、`--sort p95`） | `GET /apm/http-server-stats` |
| `apm db --service s` | DB クエリ統計 | `GET /apm/db-query-stats` |

- 30 日を超える期間を指定した場合は API に送る前にエラー。APM の 30 分上限も同様

```sh
# 直近 1 時間の 5xx を追う
mcrl log search --service web --since 1h --where 'res.status>=500' --severity error+
```

## 横断機能

| コマンド | 説明 |
|---|---|
| `api <path>` | 生 API。`-X` でメソッド、`-f` で body、`-F key=value` で型付きフィールド（GET/DELETE ではクエリ）、`--raw-field` で常に文字列のフィールド、`-H` でヘッダ、`--paginate` で `nextId` をたどって全ページ取得、`-i` でステータスとヘッダを stderr に表示。`/api/v0` は省略可 |
| `open [<url-path>]` | Web コンソールを開く（組織名は `GET /org` から取得してキャッシュ） |
| `doctor` | 設定ファイル・キーの保存場所・ベース URL への疎通・キーの有効性・時計のずれ・プロキシ設定を診断 |
| `completion <shell>` | 補完スクリプトを出力。ホスト名・監視名などは API から動的に補完（結果は短時間キャッシュ） |
| `version` | バージョン・コミット・ビルド日時。`-o json` 対応 |

```sh
mcrl api /hosts --jq '.hosts[].name'
mcrl api -X POST /graph-annotations -F title=test -F service=web -F from=now -F to=now
```

## 拡張

`PATH` 上の `mcrl-<name>` を `mcrl <name>` として実行できるようにする。
子プロセスには解決済みの `MACKEREL_APIKEY` と `MCRL_API_BASE` を渡し、プロファイル解決を再実装しなくて済むようにする。
