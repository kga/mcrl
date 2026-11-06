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
- メトリクス取得の `from` / `to` は「unix time」とだけ書かれている（秒と推定）

## ページネーション（3 方式が混在）

| 方式 | 対象 | パラメータ | 備考 |
|---|---|---|---|
| `nextId` + `limit` | `GET /alerts`、`GET /alerts/<id>/logs`、`GET /monitoring/checks` | `limit` 既定 100・最大 100（超過で 400） | レスポンスに `nextId` があれば続きあり |
| カーソル | `POST /logs` | `pageCursor: {direction: NEXT\|PREVIOUS, cursor}`、`perPage` 1–1000（既定 50） | `pagination.endCursor` / `startCursor` を使う |
| ページ番号 | `POST /traces`、`GET /apm/*` | `page`（1 始まり）、`perPage` 1–100（既定 20） | `hasNextPage` で判定 |
| なし | 上記以外の一覧系全部 | — | `GET /hosts` も上限の記述なし |

→ CLI 側でページネーションを抽象化し、利用者からは `--limit` だけ見えるようにする（[03-architecture.md](03-architecture.md)）。

## レート制限

- **全体的なレート制限は未記載**。`X-RateLimit-*` 系ヘッダの記述もない
- 明記されているのは以下のみ:
  - `POST /logs`、`POST /traces`、`GET /apm/*`: **1 リクエスト/秒**、超過で 429
  - `POST /logs` のみ `Retry-After`（秒）ヘッダを返すと明記
  - `POST /services/<name>/tsdb`: 「1 分あたりのリクエスト数超過」で 429（数値は未記載）

## エラー

- **エラーレスポンスの JSON 形式は未記載**。ステータスコードと説明文のみ
- 403 は「権限不足」と「IP 制限外からのアクセス」の両方を意味する
- AWS インテグレーションだけは権限不足・不正キーを **401** で返す
- Logs / Traces / APM は不正キーで 401
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
| メトリクス取得、Traces | 未記載 |

## ドキュメント内の矛盾・要確認

- host 監視の `warning`/`critical` 範囲: 「1-100」と「0~100」の 2 通りの記述
- external 監視: 入力は `certificationExpiration*`、レスポンス例は `certificationLimit*`
- Traces の属性フィルタ: 演算子一覧にない `CONTAINS` が例で使われている
- アラートロジのページの `nextId` 説明が「alerts」と書かれている
- alert-group-settings の作成・取得レスポンスがラップされているか不明
- 公式 OpenAPI 定義の有無は未確認
