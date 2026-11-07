# 03. アーキテクチャ

ステータス: **提案（レビュー待ち）** — [02-language.md](02-language.md) で Go に決まった前提で書いている

## 全体像

```
cmd/mcrl ─▶ internal/cli ─┬─▶ internal/resolve ─┐
  (main)     (cobra コマンド) ├─▶ internal/output   │
                          ├─▶ internal/input    ├─▶ internal/mackerel ─▶ internal/httpx ─▶ Mackerel API
                          └─▶ internal/config ──┘      (API クライアント)    (リトライ・制限・ログ)
                                 └─▶ internal/credential (キーチェーン)
```

コマンド層は「フラグを解釈し、API クライアントを呼び、結果を出力層に渡す」ことだけをする。
HTTP の詳細・出力形式・名前解決はそれぞれの層に閉じ込め、コマンドごとに再実装しない。

## パッケージ構成

| パッケージ | 責務 |
|---|---|
| `cmd/mcrl` | `main`。シグナル処理と終了コードの決定のみ |
| `internal/cli` | cobra のコマンド定義。リソースごとにファイルを分ける（`host.go`、`monitor.go` …） |
| `internal/cli/cmdutil` | コマンド共通の部品（確認プロンプト、`--dry-run`、共通フラグ登録） |
| `internal/mackerel` | API クライアント。エンドポイントごとのメソッドと型 |
| `internal/httpx` | `http.RoundTripper` の積み重ね: 認証ヘッダ、User-Agent、リトライ、レート制限、デバッグログ |
| `internal/paginate` | 3 方式のページネーションを共通のイテレータに変換 |
| `internal/output` | JSON / JSONL の出力（TTY ではインデント・色付け）、`--jq` |
| `internal/input` | `-f` のファイル読み込み（JSON/YAML/stdin）、`$EDITOR` 編集、差分表示 |
| `internal/resolve` | 名前 → ID の解決（ホスト名、監視名、ダッシュボードの URL パスなど） |
| `internal/timeexpr` | 時刻表現のパース（`-1h`、`2026-11-06T10:00`、epoch など） |
| `internal/config` | 設定ファイル・プロファイル・環境変数の解決 |
| `internal/credential` | API キーの保存・取得（キーチェーン、`api_key_command`、環境変数） |
| `internal/extension` | 拡張（`mcrl-<name>`）の探索・実行・インストール（[extensions.md](extensions.md)） |
| `internal/apply` | 宣言的管理の差分計算と適用（[07-declarative.md](07-declarative.md)） |

`internal/` に置くのは、外部から import されるライブラリとして API を約束しないため。
Go 用 Mackerel クライアントライブラリを提供することは目標にしない。

## API クライアント

### 自前で書く

既存の Go 用クライアントライブラリは使わず、`internal/mackerel` に自前で書く。理由:

- Read-Modify-Write で未知フィールドを保持するには、生 JSON を扱える API が必要
- 新しい API やフィールドへの追従を、外部ライブラリのリリースに縛られずに行える
- エンドポイント数は約 100 本で、1 本あたりのコードは小さい

### 型の方針: 「読む型」と「書く JSON」を分ける

```go
// 名前解決・フィルタ・検証用の読み取りモデル。知っているフィールドだけ持つ
type Monitor struct {
    ID     string `json:"id"`
    Type   string `json:"type"`
    Name   string `json:"name"`
    IsMute bool   `json:"isMute"`
    // 型固有のフィールドは CLI の処理に必要な分だけ
    Raw json.RawMessage `json:"-"` // API から受け取った元の JSON をそのまま保持
}

// 書き込みは生 JSON を受け取る
func (c *Client) UpdateMonitor(ctx context.Context, id string, body json.RawMessage) (*Monitor, error)
```

- 一覧・取得では型付きモデルにデコードしつつ、元の JSON（`Raw`）も保持する
- JSON 出力は `Raw` をそのまま使う。CLI の型定義に載っていないフィールドも欠けない
- 更新は `Raw` を `map[string]any` として部分変更し、そのまま PUT する
- 新規作成のためにフラグから組み立てる場合だけ、型付きの入力構造体を使う

これで Go にタグ付き共用体がない弱点が、正しさの問題にならない。

### エンドポイント定義はデータで持つ

全エンドポイントを表（Go のスライス）として持ち、そこから以下を作る。

- `mcrl api` の補完候補（パスとメソッド）
- 必要な権限（Read / Write）の表示
- 「全エンドポイントにコマンドがあるか」を確認するテスト

## HTTP 層（`internal/httpx`）

`http.RoundTripper` を積み重ねて作る。外側から:

1. **デバッグログ**: `--debug` で method・URL・ステータス・所要時間・ヘッダ・ボディを stderr に出す。`X-Api-Key` などの認証ヘッダと、JSON ボディの秘密情報（`secretKey`・`password`・`token` などのキー、`url` のホストより後ろ、外形監視の `headers`・`requestBody`）は `***` に置き換える
2. **リトライ**:
   - 対象: 429、502、503、504、接続エラー
   - 待ち時間: `Retry-After` があればそれに従う。なければ指数バックオフ＋ジッタ（0.5s 開始、上限 30s）
   - 回数: 既定 3 回（`--retries` / 設定で変更可）
   - **冪等でない POST はリトライしない**。ただし `POST /tsdb`（同時刻の値は上書き）と `POST /monitoring/checks/report`（重複は無視される）は冪等とみなしてリトライする
3. **レート制限**: クライアント側でトークンバケットを持つ
   - Logs / Traces / APM: 1 req/s（API が明記）
   - その他: 既定は制限なし。429 を受けたらそのエンドポイント群を自動で絞る
4. **認証**: `X-Api-Key` と `User-Agent: mcrl/<version> (<os>/<arch>)` を付与
5. **トランスポート**: `net/http` 標準。タイムアウト既定 30s（`--timeout`）。HTTP_PROXY 等の環境変数を尊重

## ページネーション（`internal/paginate`）

[api-notes.md](api-notes.md) の 3 方式（`nextId`、カーソル、ページ番号）を Go のイテレータ（`iter.Seq2[T, error]`）に揃える。

```go
for alert, err := range client.Alerts(ctx, mackerel.AlertsQuery{WithClosed: true}) {
    ...
}
```

コマンド側は `--limit N` 件取ったら止めるだけ。方式の違いを知らなくてよい。
JSONL の逐次出力とも相性がよい。

## エラー処理

API エラーは 1 つの型に集約する。

```go
type APIError struct {
    StatusCode int
    Method     string
    Path       string
    Message    string          // ボディから取り出せたメッセージ
    Body       json.RawMessage // 生のボディ
}
```

- エラーボディは実測で `{"error":{"message":...}}` と確認した（[api-notes.md](api-notes.md#エラー)）。これを第一に解釈し、解釈できない場合はボディの先頭をそのままメッセージにする
- メッセージの文言はエンドポイントごとにばらばらなので、エラーの種類はステータスコードだけで判定する
- 終了コードへの対応付けは `cmd/mcrl` の 1 箇所で行う（[04-cli-ux.md](04-cli-ux.md#終了コード)）

## 主要ライブラリ候補

| 用途 | ライブラリ | 理由 |
|---|---|---|
| コマンド定義・補完 | `github.com/spf13/cobra` | 動的補完、man/Markdown 生成、事実上の標準 |
| jq | `github.com/itchyny/gojq` | jq 互換、純 Go |
| YAML（`-f` と `edit` の入力のみ） | `github.com/goccy/go-yaml` | コメント・順序保持、エラー位置表示 |
| TOML（設定） | `github.com/pelletier/go-toml/v2` | 高速、厳格モード |
| JSON の色付け | 自前（トークン単位で ANSI 色を付けるだけ） | 依存を増やすほどの処理ではない |
| 対話プロンプト | `github.com/charmbracelet/huh` | 確認・選択・入力フォーム |
| ファジー選択 | `github.com/charmbracelet/bubbletea` で自作、または `github.com/ktr0731/go-fuzzyfinder` | 引数省略時のリソース選択 |
| キーチェーン | `github.com/zalando/go-keyring` | CGO 不要で 3 OS 対応 |
| TTY 判定 | `github.com/mattn/go-isatty` | |
| 差分表示 | `github.com/sergi/go-diff` など | `edit` / `apply` の差分 |
| CLI テスト | `github.com/rogpeppe/go-internal/testscript` | txtar で CLI の入出力をまとめて書ける |
| MCP サーバー（`mcrl mcp`） | `github.com/modelcontextprotocol/go-sdk`、スキーマの検証に `github.com/google/jsonschema-go` | 公式の Go SDK。stdio とテスト用のインメモリ transport がある（[decisions.md](decisions.md) D20） |

依存は最小限にし、標準ライブラリで足りるもの（HTTP、JSON、`slog`、`iter`）は標準を使う。

## ディレクトリ構成（案）

```
mcrl/
├── cmd/mcrl/main.go
├── internal/
│   ├── cli/            # コマンド定義
│   ├── mackerel/       # API クライアント
│   ├── httpx/
│   ├── paginate/
│   ├── output/
│   ├── input/
│   ├── resolve/
│   ├── timeexpr/
│   ├── config/
│   ├── credential/
│   └── apply/
├── testdata/
│   ├── script/         # testscript (*.txtar)
│   └── fixtures/       # API レスポンスの記録
├── docs/             # 利用者向けの文書（英語）。reference/ はコマンドから生成
│   └── design/         # 設計資料（日本語）
├── mise.toml           # Go・golangci-lint・goreleaser のバージョン固定とタスク
├── .goreleaser.yaml
└── go.mod              # module github.com/kga/mcrl
```
