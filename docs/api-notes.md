# Mackerel API 調査メモ

[Mackerel API ドキュメント](https://mackerel.io/api-docs/) を 2026-11-06 時点で読み、CLI 設計に効く事実だけをまとめたもの。
エンドポイント一覧そのものは公式ドキュメントを正とし、ここでは **横断的な性質・制約・ドキュメントの穴** を記録する。

> ⚠️ 「未記載」と書いたものは公式ドキュメントに記述がないもの。実装前に実 API で確認し、このファイルを更新すること。

## 基本

| 項目 | 内容 |
|---|---|
| ベース URL | `https://api.mackerelio.com/`（Mackerel for KCPS は `https://kcps-mackerel.io`） |
| パス | `/api/v0/...` |
| 認証 | `X-Api-Key` ヘッダ（必須）。キーは組織単位 |
| キー権限 | Read / Write の 2 種のみ。Read のみのキーで Write 系 API は使えない |
| 形式 | POST/PUT の入力と成功レスポンスは JSON (`application/json`) |
| 時刻 | ほぼ全て epoch 秒（number）。例外は下記 |
| エンドポイント数 | 21 カテゴリ・約 101 本 |

## 時刻表現の例外

- Logs の `results[].timestamp` は RFC 3339 文字列
- Trace の span `startTime` / `endTime` は ISO 8601 文字列（表では `endTime` が number と書かれており矛盾）
- 監視の `duration` は **型によって単位が違う**: host / external は「分」、service は「直近のポイント数」
- downtime `duration`、`notificationInterval`、`missingDuration*` は分
- メトリクス取得の `from` / `to` は「unix time」とだけ書かれているが、**秒** で確定（ミリ秒を渡すと `400 The parameter `from` is too large`）。実測の詳細は「メトリクス取得の実測」を参照

## ページネーション（3 方式が混在）

| 方式 | 対象 | パラメータ | 備考 |
|---|---|---|---|
| `nextId` + `limit` | `GET /alerts`、`GET /alerts/<id>/logs`、`GET /monitoring/checks` | `limit` 既定 100・最大 100（超過で 400） | レスポンスに `nextId` があれば続きあり |
| カーソル | `POST /logs` | `pageCursor: {direction: NEXT\|PREVIOUS, cursor}`、`perPage` 1–1000（既定 50） | `pagination.endCursor` / `startCursor` を使う |
| ページ番号 | `POST /traces`、`GET /apm/*` | `page`（1 始まり）、`perPage` 1–100（既定 20） | `hasNextPage` で判定 |
| なし | 上記以外の一覧系全部 | — | `GET /hosts` も上限の記述なし |

→ CLI 側でページネーションを抽象化し、利用者からは `--limit` だけ見えるようにする（[03-architecture.md](03-architecture.md)）。

## レート制限

- **全体的なレート制限は未記載**。`X-RateLimit-*` 系ヘッダの記述もなく、実測でも通常のレスポンスに rate / retry / limit を含むヘッダはなかった（2026-11-06、`GET /org`）。残り回数を見て事前に絞ることはできず、429 を受けてから対処するしかない
- 明記されているのは以下のみ:
  - `POST /logs`、`POST /traces`、`GET /apm/*`: **1 リクエスト/秒**、超過で 429
  - `POST /logs` のみ `Retry-After`（秒）ヘッダを返すと明記
  - `POST /services/<name>/tsdb`: 「1 分あたりのリクエスト数超過」で 429（数値は未記載）

## リクエストヘッダ

- **ボディのない DELETE でも `Content-Type: application/json` が必須**。付けないと `400 Illegal Content-Type. Please try with "Content-Type: application/json"`（実測 2026-11-06、監視・ダウンタイム・アノテーション・サービスの DELETE）。mcrl は GET/HEAD 以外に常に付ける

## エラー

ドキュメントにはステータスコードと説明文しかないが、実測（2026-11-06）で以下を確認した。

- **エラーボディは `{"error":{"message":"..."}}`**、`Content-Type: application/json`。存在しないパス（404）、不正キー（401）、バリデーション（400）のいずれも同じ形
  - 例: `{"error":{"message":"Too many limit"}}`（`GET /alerts?limit=101`）
  - 例: `{"error":{"message":"Invalid Parameter: obj.serviceName error.path.missing, ..."}}`（`POST /logs` に `{}`）
- メッセージの文言はエンドポイントごとにばらばら（`monitor is not found.`、`Host Not Found.`、`Alert not found` など）。**文言で分岐してはいけない**
- **不正なキー・キーなしは全エンドポイントで 401**（`Authentication failed. Please try with valid Api Key.`）。ドキュメント上は多くが 403 と書かれているが、403 は「権限不足」「IP 制限外」の場合
- AWS インテグレーションはドキュメント上、Write 権限不足も 401 で返す（未実測）
- **存在しない ID の GET が 400 になることがある**。形式が正しい存在しない ID は 404 だが、`xxxxxxxxxxx` のような ID は `400 Bad request.` になった（ID のデコードに失敗していると推測）。CLI では「ID による取得の 400」は「見つからない、または ID の形式が不正」として扱う
- 413: メタデータ 100KB 超過、アラートメモ 80KB 超過
- 409: ダッシュボードの `urlPath` 重複

## 一括操作の上限

| API | 上限 |
|---|---|
| `POST /hosts/bulk-update-statuses` | `ids` 最大 30 |
| `POST /hosts/bulk-retire` | `ids` 最大 30 |
| `POST /monitoring/checks/report` | `reports` 最大 100 |
| `POST /tsdb`、`POST /services/<name>/tsdb` | 未記載 |

## 更新系の注意点（Read-Modify-Write の罠）

- `PUT /monitors/<id>` は **オブジェクト全体の置き換え**。`isMute` を省略するとミュート解除される。`scopes` / `excludeScopes` も上書き。`headers` は省略時のみ「変更なし」
- `PUT /monitors/<id>` で `type` は変更不可（400）
- `PUT /channels/<id>` で `type` 変更不可、`addToDefaultNotificationGroup` は更新時指定不可
- `PUT /hosts/<id>` は roleFullnames に含めなかったロールを **外さない**。外すには `PUT /hosts/<id>/role-fullnames`
- `PUT /hosts/<id>/role-fullnames` はロール一覧の置き換え
- `PUT /aws-integrations/<id>` は `key` / `secretKey` / `roleArn` / `externalId` 省略時は変更なし、`null` で認証方式切替、他は上書き
- `PUT /notification-groups/<id>` でデフォルトグループの改名は不可

→ 「取得 → 一部変更 → PUT」を CLI が行う場合は、**型付き構造体を経由せず生 JSON のまま扱い、未知フィールドを落とさない** こと。

## 書き込み系の特殊な振る舞い

- メトリクス投稿: 24 時間より古いタイムスタンプは **200 を返しつつ黙って捨てられる**
- チェック監視レポート: `occurredAt` が 6 時間以上未来、または同一 host/name で投稿済みのものは無視
- サービス・ロール名: `^[A-Za-z0-9][A-Za-z0-9_-]+$`、2–63 文字
- メトリクス名: `[a-zA-Z0-9._-]+`
- グラフ定義名: `^custom(\.([-a-zA-Z0-9_]+|[*#]))+$`
- メタデータ namespace: `[-a-zA-Z0-9_]+`、`mackerel` で始まる名前は予約
- メタデータ数上限: host 50 / service 50 / role 10、値は任意の JSON 100KB まで
- サービス削除はロール・サービスメトリクス・監視設定・グラフアノテーションを **連鎖削除**
- 退役済みホストへのメタデータ書き込みは 400、退役 1 週間超のホストのメタデータ読み取りも 400

## パスの不揃い

- チェック監視は `/api/v0/monitoring/checks`（`/monitors` 配下ではない）
- 招待の取り消しは `POST /invitations/revoke`（DELETE ではない）
- グラフ定義の作成は `POST /graph-defs/create`、削除は `DELETE /graph-defs`（body に name）
- Logs / Traces の一覧は検索条件が多いため POST
- AWS インテグレーション一覧のキーは `aws_integrations`（スネークケース）
- `GET /hosts.json` は `GET /hosts` と同一

## ID

- 監視・通知チャンネル・通知グループ・ユーザー・ホストの ID は **11 文字の英数字**（実測）
- 名前解決では「11 文字の英数字なら ID とみなし、それ以外は名前」とする。ホスト名がたまたま 11 文字の英数字の場合に備え、`name:` 接頭辞で強制できるようにする
- `GET /hosts?name=` は **完全一致・大文字小文字を区別**（`mcrl-fake` や大文字では 0 件）。部分一致の検索が要る場合は CLI 側で一覧を取って絞る
- mackerel-agent が登録したホストの `displayName` と `customIdentifier` は `null`、`roles` は `{}`
- `GET /hosts-by-custom-identifier/<cid>` は該当なしでも 404 ではなく **200 で `{"host": null}`** を返す（実測）
- `GET /monitors/<id>` は `{"monitor": {...}}` でラップされる（実測）

## 退役したホスト（実測 2026-11-06）

- `POST /hosts/<id>/retire` の後、`GET /hosts/<id>` は `isRetired: true` と `retiredAt` 付きで取れる。`status` は退役前の値（`working` など）のまま
- `GET /hosts`（全ステータス指定でも）と `GET /hosts?name=` には出てこない。そのため名前では解決できず、`id:` で指定する必要がある
- 退役したホストに付いていたチェック監視（`GET /monitoring/checks`）は残る。チェック監視を消す API はない

## 書き込み系の実測（2026-11-06）

- `PUT /hosts/<id>/role-fullnames` の後のホストの `roles` は、サービスごとのロール名の配列（順序は追加順ではない）
- `POST /downtimes` / `PUT /downtimes/<id>` は入力に `id` を足したものを返す。`recurrence.until` もそのまま返る
- `PUT /graph-annotations/<id>` は全項目の再指定が必要（単体を取得する API がないため、mcrl でもマージできない）
- `check report` で WARNING、`check run` で CRITICAL を報告しても、3 分待ってもアラートは作られなかった（原因未確認）

## 列挙値

- ホストステータス（書き込み可）: `working` / `standby` / `maintenance` / `poweroff`
- ホスト一覧の `status` 既定: `working` と `standby`
- 監視ステータス: `OK` / `WARNING` / `CRITICAL` / `UNKNOWN`
- 監視種別: `host` / `connectivity` / `service` / `external` / `expression` / `anomalyDetection` / `query`（`check` はアラート上のみ登場、監視 API では作れない）
- 通知チャンネル種別: `email` / `email-broadcast` / `slack` / `chatwork` / `twilio` / `pagerduty` / `opsgenie` / `jira-service-management` / `microsoft-teams` / `webhook` / `amazon-event-bridge`
  - **API で作成・更新できるのは `email` / `slack` / `webhook` のみ**
- 通知イベント: `alert` / `alertGroup` / `hostStatus` / `hostRegister` / `hostRetire` / `monitor`
- ユーザー権限: `owner` / `manager` / `collaborator` / `viewer`（招待できるのは owner 以外）
- ログ severity: `UNSPECIFIED` / `TRACE` / `DEBUG` / `INFO` / `WARN` / `ERROR` / `FATAL`

## 期間の上限

| API | 上限 |
|---|---|
| `POST /logs` | `from`〜`to` 最大 30 日。`to` は排他 |
| `GET /apm/*` | 最大 30 分 |
| メトリクス取得 | 未記載。400 日の範囲でも 200 が返った（実測） |
| Traces | 未記載 |

## メトリクス取得の実測

2026-11-06、mackerel-agent 0.87.2 を入れた擬似ホストで `GET /hosts/<id>/metrics` を確認した。

- `from` と `to` は **両方必須**。片方だけ・両方なしは `400 Bad request.`
- `to < from` は 400 にならず、`{"metrics": []}` が返る。CLI 側で事前に弾く
- 範囲が短いと 1 分刻みの生データ。**範囲が 1 日以上になると、粗い粒度に集約された点が返る**（時刻も丸められる）。エージェントを動かした時間が短く、範囲ごとの正確な粒度までは確認できていない
- 存在しないメトリクス名は `404 metric not found`
- mackerel-agent（Linux）が標準で送るメトリクスは 28 種（`cpu.*.percentage`、`loadavg5` など）
- `GET /tsdb/latest` の値は `{"<hostId>": {"<name>": {"time", "value"}}}` の形（ドキュメントどおり）

## ドキュメント内の矛盾・要確認

- host 監視の `warning`/`critical` 範囲: 「1-100」と「0~100」の 2 通りの記述
- external 監視: 入力は `certificationExpiration*`、レスポンス例は `certificationLimit*`
- Traces の属性フィルタ: 演算子一覧にない `CONTAINS` が例で使われている
- アラートロジのページの `nextId` 説明が「alerts」と書かれている
- alert-group-settings の作成・取得レスポンスがラップされているか不明（一覧は `{"alertGroupSettings": [...]}` を実測。単体取得は設定 0 件のため未確認）
- 保存されたログ検索の一覧のキーは `results`（他の一覧はリソース名の複数形）
- 公式 OpenAPI 定義の有無は未確認
