# 06. 認証と設定

ステータス: **提案（レビュー待ち）**

## 要件

- 複数の組織（本番・検証、個人・会社）を切り替えて使える
- KCPS など別のベース URL を使える
- API キーを平文でディスクに置かないのが既定
- CI・コンテナでは環境変数だけで動く
- 1Password などのシークレット管理ツールから直接キーを取れる

## プロファイル

1 プロファイル = 1 組織への接続設定（ベース URL + API キーの取り方 + 既定値）。

```toml
# ~/.config/mcrl/config.toml
default_profile = "work"

[profiles.work]
# base_url の既定は https://api.mackerelio.com
# API キーは `mcrl auth login --profile work` でキーチェーンに保存済み

[profiles.work.defaults]
service = "web"          # --service の既定値

[profiles.staging]
api_key_command = "op read op://Infra/mackerel-staging/credential"

[profiles.kcps]
base_url = "https://kcps-mackerel.io"
```

## API キーの解決順

上から順に見て、最初に見つかったものを使う。

| 順 | 取得元 | 用途 |
|---|---|---|
| 1 | 環境変数 `MACKEREL_APIKEY` | CI・コンテナ・一時的な上書き。Mackerel 周辺ツール向けに設定済みの環境もそのまま使える |
| 2 | プロファイルの `api_key_command`（実行結果の stdout） | 1Password・Vault などとの連携 |
| 3 | OS のキーチェーン（サービス名 `mcrl`、アカウント名 = プロファイル名） | 手元の既定 |
| 4 | プロファイルの `api_key`（平文） | キーチェーンがない環境の最終手段。ファイル権限が 0600 でなければ警告 |

- **コマンドラインフラグで API キーを渡す方法は用意しない**（シェル履歴・`ps` に残るため）
- どこから取得したかは `mcrl auth status` と `--debug` で分かる
- キーチェーンが使えない環境（ヘッドレス Linux など）で `auth login` した場合は、平文保存してよいか確認する
- 保存先が 2 か所（キーチェーンと設定ファイル）あるので、`auth login` / `auth logout` は途中で失敗しても状態が食い違わないようにする
  - `auth login` はキーチェーンに入れた後で設定ファイルを保存できなければ、キーチェーンを元のキー（なければエントリなし）に戻す
  - 平文で保存したときは、キーチェーンに残った古いキーを消す。キーチェーンは平文より優先されるので、残すと新しいキーが使われない
  - `auth logout` は平文のキーを先に消し、次にキーチェーンから消す。キーチェーンにあるのに消せなければ失敗として終了コードで知らせる（読めないキーチェーンには保存されていないとみなし、警告だけ出す）

### API キーの環境変数

API キーを渡す環境変数は `MACKEREL_APIKEY` の 1 つだけ（[decisions.md](decisions.md) D5）。
mcrl 独自の変数（`MCRL_API_KEY` など）は作らず、`MACKEREL_API_KEY` など他の綴りも読まない。

- 環境変数のキーは `--profile` で選んだプロファイルより優先される。別組織のキーを意図せず使う事故を防ぐため、`--profile` を明示したのに環境変数のキーを使った場合は stderr に警告を出す
- どの変数を読んだかは `mcrl auth status` と `--debug` で表示する

## プロファイルの選択順

1. `--profile` フラグ
2. 環境変数 `MCRL_PROFILE`
3. 設定ファイルの `default_profile`
4. `default`

### ディレクトリごとにプロファイルを固定する

リポジトリごとに組織を固定したいときは、mise や direnv などのディレクトリ単位で環境変数を設定するツールで `MCRL_PROFILE` を設定する。mcrl 自身はディレクトリを辿って設定ファイル（当初案の `.mcrl.toml`）を探さない（[decisions.md](decisions.md) D24。D8 を置き換える）。

```toml
# プロジェクトの mise.toml
[env]
MCRL_PROFILE = "staging"
```

```sh
# direnv の .envrc
export MCRL_PROFILE=staging
```

どこで決まったかは `mcrl auth status` の `profile.source` で分かる（`MCRL_PROFILE` なら `env`）。

## ファイルの置き場所

XDG Base Directory に従う。macOS でも `~/Library/Application Support` ではなく `~/.config` を使う（CLI 利用者の慣習に合わせる）。

| 種類 | パス | 上書き |
|---|---|---|
| 設定 | `$XDG_CONFIG_HOME/mcrl/config.toml`（既定 `~/.config/mcrl/config.toml`） | `MCRL_CONFIG` |
| キャッシュ（名前解決・補完） | `$XDG_CACHE_HOME/mcrl/`（既定 `~/.cache/mcrl/`） | `MCRL_CACHE_DIR` |

Windows は `%APPDATA%\mcrl\config.toml`、`%LOCALAPPDATA%\mcrl\cache\`。

## 環境変数一覧

| 変数 | 説明 |
|---|---|
| `MACKEREL_APIKEY` | API キー |
| `MCRL_API_BASE` | ベース URL |
| `MCRL_PROFILE` | プロファイル |
| `MCRL_CONFIG` | 設定ファイルのパス |
| `MCRL_OUTPUT` | 既定の出力形式 |
| `MCRL_COLOR` / `NO_COLOR` | JSON の色付け |
| `MCRL_NO_INPUT` | 対話しない |
| `MCRL_DEBUG` | デバッグ出力 |
| `MCRL_CACHE_DIR` | キャッシュの場所 |
| `VISUAL` / `EDITOR` | `edit` で使うエディタ |
| `HTTPS_PROXY` / `NO_PROXY` | プロキシ |

## キャッシュ

- 名前 → ID の対応（ホスト・監視など）と組織名を、プロファイル単位で短時間（既定 5 分）キャッシュする
- 用途は補完と `open` の高速化。**更新系コマンドの名前解決ではキャッシュを使わない**（古い対応で別のリソースを消す事故を防ぐ）
- `mcrl config clear-cache` で削除

## 権限の扱い

API からはキーが Read か Write かを直接取得できない。そのため:

- `auth status` では権限を表示しない（推測で表示しない）
- Write 権限が要るコマンドで 403 を受けたら、「キーに Write 権限がない、または IP 制限の範囲外」と両方の可能性を hint に出す
- ヘルプに各コマンドが必要とする権限を書く
