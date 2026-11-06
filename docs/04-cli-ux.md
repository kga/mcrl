# 04. CLI の UX 仕様

ステータス: **提案（レビュー待ち）**

コマンドの一覧は [05-commands.md](05-commands.md)。ここでは全コマンドに共通する約束ごとを決める。

## 文法

```
mcrl <リソース> <動詞> [位置引数...] [フラグ...]
```

- リソースは **単数形**（`host`、`monitor`）。複数形は別名として受け付ける（`hosts`）
- 標準の動詞と別名:

| 動詞 | 別名 | 意味 |
|---|---|---|
| `list` | `ls` | 一覧 |
| `get` | `show` | 1 件取得 |
| `create` | `new` | 作成 |
| `update` | — | フラグまたはファイルで更新 |
| `edit` | — | `$EDITOR` で開いて更新 |
| `delete` | `rm` | 削除 |

- リソース固有の動詞はその都度定義する（`host retire`、`alert close`、`check run`）
- サブコマンドの深さは原則 2 段まで（`mcrl <リソース> <動詞>`）。3 段目は `metric graph-def create` のような明確な下位リソースだけ

## グローバルフラグ

| フラグ | 環境変数 | 説明 |
|---|---|---|
| `--profile <name>` | `MCRL_PROFILE` | 使うプロファイル（[06-auth-config.md](06-auth-config.md)） |
| `-o, --output <fmt>` | `MCRL_OUTPUT` | `json`（既定） / `jsonl` |
| `--jq <expr>` | — | 出力 JSON に jq 式を適用。結果が文字列なら引用符なしで出す（`jq -r` 相当） |
| `--compact` | — | TTY でも 1 行の JSON で出す |
| `--color <when>` | `NO_COLOR`、`MCRL_COLOR` | JSON の色付け。`auto` / `always` / `never` |
| `--debug` | `MCRL_DEBUG` | HTTP のやりとりを stderr に出す |
| `--dry-run` | — | 更新系: 送信予定のリクエストを表示して終了 |
| `-y, --yes` | — | 確認プロンプトを省略 |
| `--no-input` | `MCRL_NO_INPUT` | 対話入力を一切しない（必要な情報がなければエラー） |
| `--timeout <dur>` | — | 1 リクエストのタイムアウト（既定 30s） |

フラグは位置を問わず受け付ける（`mcrl -o json host list` も `mcrl host list -o json` も可）。

## 出力

### JSON だけを出す

**出力は JSON（と JSONL）だけ。表・YAML・CSV などの人間向け形式は持たない**（[decisions.md](decisions.md) D6）。

- 理由: 出力が API のレスポンスと 1 対 1 になり、API ドキュメントがそのまま出力の仕様書になる。形式ごとの描画・列定義・互換性の管理も要らない
- 見た目の違いは TTY かどうかだけ:
  - stdout が TTY → インデント付き・色付き
  - stdout が TTY でない → 1 行の JSON（`--compact` で TTY でも 1 行）
- 読みやすく絞りたいときは `--jq` を使う（下記）

### JSON 出力の約束

- **一覧は配列**、**単体はオブジェクト**。API のラップ（`{"hosts": [...]}` など）は剥がす
- 中身は API が返した JSON をそのまま出す。フィールド名・値・時刻（epoch 秒）を CLI が加工しない
  - 理由: API ドキュメントがそのまま出力の仕様書になる。CLI 独自の変換を覚えなくてよい
- 更新系コマンドは、API が返したオブジェクトを出す。`{"success": true}` しか返らない API では、可能なら更新後のリソースを取得し直して出す（`--no-refetch` で抑止）
- `jsonl` は一覧の 1 要素を 1 行で出す。ページングしながら逐次出力するので、大量のアラートやログでもメモリを使わない。`log tail` のように終わりのないコマンドは常に `jsonl`
- `mcrl api` だけは API のレスポンスを無加工（ラップ付き）で出す
- CLI が別の API から情報を足すこと（アラートに監視名を付けるなど）は既定ではしない。必要なコマンドだけ `--expand` で、元のオブジェクトに `_expanded` キーを足す形で行う（API のフィールドと混ざらないように）

### `--jq` とパイプ

`--jq` の結果が文字列のときは引用符なしで 1 行ずつ出す。これで ID を次のコマンドに渡せる。

```sh
mcrl host list --status poweroff --jq '.[].id' | mcrl host retire --yes -
mcrl alert list --jq '.[] | select(.status == "CRITICAL") | .id'
mcrl alert list --jq '.[] | [.id, .status, (.openedAt | todate)] | @tsv'
```

位置引数に `-` を渡すと、stdin から 1 行 1 件で読む。これを全コマンドで共通にする。

### stdout と stderr

- stdout: 結果のデータだけ
- stderr: 進捗、警告、エラー、プロンプト
- `mcrl ... 2>/dev/null` で常に JSON の結果だけが得られる

## 入力

### フラグとファイル

作成・更新は 2 通りの入力を受け付ける。

```sh
# よく使う項目はフラグで
mcrl downtime create --name "定期メンテ" --start "2026-11-10 02:00" --duration 2h --service web

# 全項目はファイルで（JSON / YAML、- で stdin）
mcrl monitor create -f monitor.yaml
```

- `-f` とフラグを併用した場合はフラグが優先（ファイルの値を上書き）
- YAML は JSON に変換してから送る。YAML のキーは API の JSON と同じ名前（camelCase）
- `mcrl <リソース> template [<種別>]` で、そのリソースの雛形 YAML をコメント付きで出す

### `$EDITOR` での編集

`mcrl monitor edit <監視>` の流れ:

1. 現在の設定を取得し、読み取り専用フィールド（`id` など）を除いた YAML にする
2. `$VISUAL` → `$EDITOR` → `vi` の順でエディタを開く
3. 保存後に検証し、エラーがあればエラー位置をコメントで書き込んで再度開く
4. 差分を表示し、確認後に PUT。差分がなければ何もしない

元の JSON に CLI が知らないフィールドがあってもそのまま保持して送る（[03-architecture.md](03-architecture.md#型の方針-読む型と書く-json-を分ける)）。

## リソースの指定（名前解決）

ID を覚えて打つのはつらいので、名前でも指定できるようにする。

| リソース | 受け付ける指定 | 解決方法 |
|---|---|---|
| ホスト | ID、ホスト名、`cid:<customIdentifier>` | ID 形式ならそのまま。それ以外は `GET /hosts?name=` / `GET /hosts-by-custom-identifier/` |
| 監視 | ID、監視名 | 一覧を取得して名前で照合 |
| ダッシュボード | ID、URL パス、タイトル | 一覧を取得して照合 |
| 通知チャンネル・通知グループ・アラートグループ・ダウンタイム | ID、名前 | 一覧を取得して照合 |
| ロール | `<サービス>:<ロール>` | — |

- 名前が複数に一致した場合はエラーにし、候補の ID を一覧で出す（勝手に 1 つを選ばない）
- `id:<値>` / `name:<値>` の接頭辞で解釈を強制できる
- TTY で引数を省略した場合はファジー選択を出す。`--no-input` 時と非 TTY ではエラー

## 時刻と期間

時刻を受け取るフラグ（`--since`、`--until`、`--start`、`--from`、`--to`）は全て同じパーサで解釈する。

| 書き方 | 意味 |
|---|---|
| `1h`、`30m`、`7d` | 今から遡った時刻（`--since 1h` = 1 時間前から） |
| `-1h`、`+30m` | 今からの相対時刻 |
| `now` | 現在 |
| `2026-11-06T10:00:00+09:00` | RFC 3339 |
| `2026-11-06 10:00`、`10:00` | ローカルタイムゾーンとして解釈（時刻のみなら今日） |
| `1762390800` | epoch 秒 |
| `@1762390800` | epoch 秒（明示） |

期間を受け取るフラグ（`--duration`、`--interval`）は `90m`、`2h`、`1d12h` の形式。API が分を要求する場合は CLI が変換する。

`--tz <zone>` / `TZ` で表示・解釈のタイムゾーンを変えられる。

## 対話と非対話

- 確認プロンプトを出すのは **stdin と stderr がともに TTY** のときだけ
- 非 TTY で確認が必要な操作は、`--yes` がなければ終了コード 2 でエラーにする（勝手に進めない）
- `--no-input` は TTY でも対話しない。CI・AI エージェント向け
- 長い処理（ページングで数千件取得など）は stderr にスピナーを出す。非 TTY では出さない

## エラー表示

```
error: monitor "cpu high" not found
  hint: run `mcrl monitor list` to see available monitors
```

- 1 行目は `error: ` で始まる要約。2 行目以降に `hint:` で次の行動を示す
- API エラーは `(HTTP 404 GET /api/v0/monitors/xxx)` のようにステータスとエンドポイントを添える
- stderr が TTY でないときは、同じ内容を 1 行の JSON で出す:

```json
{"error":{"code":"not_found","message":"monitor \"cpu high\" not found","status":404,"hint":"..."}}
```

## 終了コード

| コード | 意味 | 例 |
|---|---|---|
| 0 | 成功 | |
| 1 | その他のエラー | 想定外のエラー、5xx でリトライを使い切った |
| 2 | 使い方の誤り | 不正なフラグ、必須引数なし、非 TTY で `--yes` なし |
| 3 | 認証・認可 | API キーなし、401、403 |
| 4 | 見つからない | 404、名前解決で 0 件 |
| 5 | 入力値の不正・競合 | 400、409、413、名前解決で複数一致 |
| 6 | レート制限 | リトライを使い切っても 429 |
| 7 | ネットワーク | 接続エラー、タイムアウト |
| 130 | 中断 | Ctrl-C |

`mcrl check run` のように子コマンドを実行するものは、子の終了コードをそのまま返す（ドキュメントに明記）。

## ヘルプ

- `mcrl <リソース> <動詞> --help` に、必ず **実行例** を 2 つ以上載せる
- 必要な API キー権限（Read / Write）と、対応する API エンドポイントをヘルプ末尾に表示する
- `mcrl help environment` で環境変数の一覧、`mcrl help exit-codes` で終了コードの一覧
- ヘルプとメッセージは英語のみ（[decisions.md](decisions.md) D3）。この文書中の日本語の例は説明用
