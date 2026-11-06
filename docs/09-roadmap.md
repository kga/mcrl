# 09. ロードマップ

ステータス: **提案（レビュー待ち）**

各マイルストーンは「そこで止めても使えるもの」になるように切る。

## M0: 土台（2026-11-06 完了）

`config` は `config path` のみ。`--paginate` は `nextId` 方式のみ対応。

- リポジトリ構成、`mise.toml`、CI、GoReleaser（リリースが通るところまで）
- `internal/httpx`（認証・リトライ・デバッグログ）、`internal/output`（json / jsonl、`--jq`）
- `internal/config`・`internal/credential`、`auth login/logout/status`
- `mcrl api`（この時点で全 API を叩ける）
- `version`、`completion`、`doctor`
- エラー表示と終了コード

## M1: 参照系（障害対応で使える）（2026-11-06 完了）

次は M2 に回した: stdin からの `-`（複数 ID を受ける更新系と一緒に入れる）、`downtime list --active`、補完結果のキャッシュ。M1 では `check list/get`、`aws list/get`、各リソースの `get` も入れた。

- `org get`、`service list`、`role list`
- `host list/get/monitor-status`
- `monitor list/get`、`alert list/get/logs`
- `metric get/latest/names`
- `downtime list`、`channel list`、`notification-group list`、`alert-group list`、`dashboard list/get`、`annotation list`、`user list`
- 名前解決、stdin からの `-`、`--expand`、動的補完

## M2: 更新系（日々の運用で使える）

2026-11-06 に前半（M2a）を実装: 確認プロンプト・`--dry-run`・取り直し・stdin の `-`・`-f`（JSON/YAML）の共通部分、`alert close/memo`、`host status/retire/role set|add|remove`、`service create/delete`、`role create/delete`、`monitor create/update/delete/mute/unmute/template`、`downtime create/update/delete`、`metric post`、`check report/run`、`annotation create/update/delete/wrap`。

2026-11-07 に後半（M2b）を実装: `edit`（monitor・downtime・channel・notification-group・alert-group・dashboard・aws。YAML で開き、差分を見せて確認してから送る）、channel・notification-group・alert-group・dashboard（配置の検証つき）・aws の create/update/delete、`aws external-id/excludable-metrics`、`metadata list/get/set/delete`、`user remove`、`invitation list/create/revoke`、`org ip-restriction get/set`、`downtime list --active`、補完結果のキャッシュ（5 分、`config clear-cache`）。

M2 で見送ったもの: `host create/update/edit`（ホストは mackerel-agent が登録・更新するもので、`PUT /hosts/<id>` は meta などを丸ごと置き換えるため事故が起きやすい。必要になったら設計し直す）、`config get/set/list/edit`、`open`。

当初の計画:

- `alert close/memo`、`host status/retire/role`、`downtime create/update/delete`
- `monitor create/update/edit/delete/mute/unmute/template`
- `metric post`、`check report/run`、`annotation create/wrap`
- 残りのリソースの CRUD（channel、notification-group、alert-group、dashboard、metadata、service、role、user、invitation、aws、org ip-restriction）
- `edit` の基盤（`$EDITOR`、差分、検証）、`--dry-run`、確認プロンプト

## M3: オブザーバビリティ

- `log search/tail/saved`
- `trace list/get`
- `apm http/db`

## M4: 宣言的管理（スコープに含める場合）

- `export`、`diff`、`apply`（[07-declarative.md](07-declarative.md)）

## M5 以降の候補

- `mcrl mcp`: MCP サーバーとして起動し、AI エージェントにツールとして公開
- プラグイン（`mcrl-<name>`）の仕組みの整備
- ディレクトリ単位の `.mcrl.toml` によるプロファイル固定（[decisions.md](decisions.md) D8）

## 1.0.0 の条件

- 全エンドポイントに対応するコマンドがある
- [08-quality-release.md](08-quality-release.md#バージョニングと互換性) の公開インターフェースを固められた
- 実 API での e2e テストが主要コマンドで通っている
