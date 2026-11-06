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

## M1: 参照系（障害対応で使える）

- `org get`、`service list`、`role list`
- `host list/get/monitor-status`
- `monitor list/get`、`alert list/get/logs`
- `metric get/latest/names`
- `downtime list`、`channel list`、`notification-group list`、`alert-group list`、`dashboard list/get`、`annotation list`、`user list`
- 名前解決、stdin からの `-`、`--expand`、動的補完

## M2: 更新系（日々の運用で使える）

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
