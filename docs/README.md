# mcrl 設計ドキュメント

Mackerel API のための新しい CLI「mcrl」の設計資料。

| ドキュメント | 内容 | ステータス |
|---|---|---|
| [01-vision.md](01-vision.md) | 目的・利用者・設計原則・非目標 | 提案 |
| [02-language.md](02-language.md) | 実装言語の比較と選定 | 決定（Go） |
| [03-architecture.md](03-architecture.md) | パッケージ構成・API クライアント・HTTP 層・ライブラリ | 提案 |
| [04-cli-ux.md](04-cli-ux.md) | 文法・グローバルフラグ・出力・入力・名前解決・時刻・終了コード | 提案 |
| [05-commands.md](05-commands.md) | 全コマンドとエンドポイントの対応 | 提案 |
| [06-auth-config.md](06-auth-config.md) | プロファイル・API キーの保存と解決・設定ファイル | 提案 |
| [07-declarative.md](07-declarative.md) | export / diff / apply | 構想 |
| [08-quality-release.md](08-quality-release.md) | テスト・配布・互換性・セキュリティ | 提案 |
| [09-roadmap.md](09-roadmap.md) | マイルストーン | 提案 |
| [decisions.md](decisions.md) | 決定事項と未決事項の台帳 | 随時更新 |
| [api-notes.md](api-notes.md) | Mackerel API の調査メモ（制約・不揃い・ドキュメントの穴） | 随時更新 |

## 参照してよい資料

- Mackerel API ドキュメント: https://mackerel.io/api-docs/
- Mackerel ヘルプ: https://mackerel.io/ja/docs/

**既存の Mackerel CLI（mkr）のソースコード・ドキュメントは参照しない**（[decisions.md](decisions.md) D1）。
