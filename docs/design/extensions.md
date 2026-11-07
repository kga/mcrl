# 拡張

mcrl の本体に入れない機能（通知の避難訓練、監視しきい値の過去検証など）を、本体の外で作って `mcrl <name>` として呼べるようにする仕組み。決定の経緯は [decisions.md](decisions.md) D23。

## 仕組み

`mcrl <name> ...` の `<name>` が本体のコマンドでも別名でもなければ、mcrl は `mcrl-<name>` という実行ファイルを次の順で探して実行する。

1. 拡張ディレクトリ: `$MCRL_EXTENSIONS_DIR`、なければ `$XDG_DATA_HOME/mcrl/extensions`（既定 `~/.local/share/mcrl/extensions`。Windows は `%LOCALAPPDATA%\mcrl\extensions`）の `mcrl-<name>/mcrl-<name>`
2. `PATH` の各ディレクトリの `mcrl-<name>`（相対パスのエントリは見ない）

- 残りの引数は解釈せずそのまま渡す。`mcrl drill --help` の `--help` も拡張に届く
- 標準入出力はそのままつなぎ、拡張の終了コードを mcrl の終了コードにする。シグナルで終わったときは `128 + シグナル番号`
- Ctrl-C は端末がプロセスグループ全体に SIGINT を送るので、拡張に直接届く。mcrl は SIGINT で終わらずに拡張の終了を待つ（mcrl からも送ると二重に届くため転送しない）。mcrl だけに送られることが多い SIGTERM と SIGHUP は拡張に転送する
- 本体のコマンド・別名・`help`・`completion` が常に優先する。`mcrl-host` を置いても `mcrl host` は本体のまま。`mcrl extension install` も本体と同じ名前の拡張を断る
- 拡張の名前は英小文字・数字・ハイフン（`^[a-z0-9][a-z0-9-]*$`）
- 中身はシェルスクリプト・Python・Go のバイナリなど何でもよい。Unix では実行権限が要る。Windows では `PATHEXT` の拡張子（`.exe`・`.cmd`・`.bat` など）が付いたものを探し、拡張ディレクトリの拡張子なしのファイル（shebang 付きのスクリプト）は `PATH` の `sh`（Git for Windows のもの）で動かす

### グローバルフラグ

拡張の名前より前に置けるグローバルフラグは、環境変数として拡張に渡せるものだけ。

| フラグ | 拡張に渡る環境変数 |
|---|---|
| `--profile` | `MCRL_PROFILE`（フラグがなくても解決済みの値を渡す） |
| `-o` / `--output` | `MCRL_OUTPUT` |
| `--color` | `MCRL_COLOR` |
| `--debug` | `MCRL_DEBUG` |
| `--no-input` | `MCRL_NO_INPUT` |

`--jq`・`--compact`・`--dry-run`・`--yes`・`--timeout` を名前の前に置くと使い方の誤り（終了コード 2）にする。これらの意味は拡張ごとに違いうるので、拡張が受け付けるなら名前の後ろに書く（`mcrl drill --dry-run`）。

渡した環境変数は、拡張から呼ぶ mcrl もそのまま読む。`mcrl --profile prod drill` の中で `"$MCRL_BIN" api /org` を呼べば prod の組織に向く。

### 拡張に渡す環境変数

| 変数 | 内容 |
|---|---|
| `MCRL_BIN` | mcrl 本体の絶対パス。拡張から mcrl を呼ぶときはこれを使う（`PATH` の別の mcrl を呼ばないため） |
| `MCRL_PROFILE` | 解決済みのプロファイル名（`--profile` → `MCRL_PROFILE` → `default_profile` → `default`） |
| `MCRL_API_BASE` | そのプロファイルの API のベース URL |

ほかの環境変数は mcrl の環境をそのまま受け継ぐ。

**API キーそのものは渡さない。** 拡張が API を呼ぶときは `"$MCRL_BIN" api ...` や `"$MCRL_BIN" host list` のように本体を呼び、キーの解決（キーチェーン、`api_key_command`、`MACKEREL_APIKEY`）は本体に任せる。理由:

- 拡張は他人が書いたコードであることが多い。キーを環境変数に置くと、拡張がさらに起動するプロセスすべてに継承され、ログやクラッシュレポートにも出やすい。キーチェーンや `api_key_command` で保管している意味がなくなる
- 本体経由なら、リトライ・レート制限・ページング・エラーの分類・`--debug` のログ（キーは伏せる）が拡張でもそのまま効く
- 当初の構想（解決済みの `MACKEREL_APIKEY` を渡す）は、拡張がキーを直接使えて楽だが、上の理由でやめた

ユーザーが自分で `MACKEREL_APIKEY` を設定している場合は、ほかの環境変数と同じく拡張にも見える。mcrl はそれを取り除きはしない（拡張から呼ぶ mcrl が使うため）。

`MCRL_API_BASE` は mcrl 自身も読む上書き用の変数なので、拡張の中で別のプロファイルの mcrl を呼ぶ（`"$MCRL_BIN" --profile other ...`）ときは、`MCRL_API_BASE` を外すか、その組織の URL にする必要がある。拡張は呼ばれたときの接続先で動くもの、という扱い。

## 管理コマンド

出力はほかのコマンドと同じ JSON（D6）。

| コマンド | 説明 |
|---|---|
| `extension list` | 拡張ディレクトリと `PATH` の拡張。`source` は `git`（install で clone したもの）・`local`（リンクしたディレクトリ）・`path`（`PATH` にあるもの）。`git` は `url` と `commit`（チェックアウトしているコミット）も出す。本体と同じ名前で動かないものは `shadowed: true` |
| `extension install <owner/repo \| url \| path>` | GitHub の `owner/repo`（`https://github.com/owner/repo.git`）、任意の git URL、ローカルのディレクトリから入れる。出力にインストールしたコミットが出る |
| `extension upgrade <name>` | clone した拡張を、リモートの既定ブランチの最新コミットにする。`--dry-run` なら取得して新しいコミットを見せるだけ |
| `extension remove <name>` | clone は削除、リンクはリンクだけ消す |

別名は `ext`。

- リポジトリ・ディレクトリの名前は `mcrl-<name>` にし、その直下に実行ファイル `mcrl-<name>` を置く
- clone は `git clone --depth 1`（既定ブランチの最新コミット）。clone したコミットが「インストールしたコミット」で、`upgrade` するまで動かない。mcrl が裏で更新を確かめることもない（D11 と同じ考え）
- `upgrade` は `git fetch --depth 1 origin HEAD` のあと `git reset --hard FETCH_HEAD`。clone は mcrl のものとして扱い、中の変更は捨てる（手を入れたいならローカルのディレクトリとして入れる）。強制 push されたリポジトリにも追従する
- ローカルのディレクトリはシンボリックリンクで入れるので、開発中の変更がすぐ効く。同じ綴りのディレクトリがあれば `owner/repo` よりディレクトリを優先する。GitHub を指したいときは URL で書く
- `PATH` にある拡張は mcrl の管理外。`upgrade`・`remove` はできないので、入れた方法で更新・削除する
- 本体と同じ名前、すでに入っている名前、直下に実行ファイルがないものは入れない

### git の前提

- リポジトリからの install・upgrade と、list の `url`・`commit` には `git` コマンドが要る。gh は使わない
- private リポジトリは git の認証設定に従う。GitHub なら `gh auth setup-git` などで git の credential helper を設定しておく
- バイナリのリリースを取ってくる機能はない。Go などでビルドする拡張は、clone 後に動く形（ビルド済みのファイルをコミットする、初回に自分でビルドするスクリプトにする）にするか、`go install` などで `PATH` に置く

## 安全について

拡張は **自分の権限で他人のコードを動かす** ことになる。キーを直接渡さなくても、拡張は `$MCRL_BIN` を通して API キーの権限でなんでもできる（Write 権限のキーなら削除も）。

- 入れる前にコードを読む。`install` の出力の `commit` が読んだコミットと同じか確かめる
- `upgrade` の前に `--dry-run` で新しいコミットを確かめ、差分を読んでから上げる
- 本体のコマンドを乗っ取られないように、本体の名前は常に優先する
- `PATH` の相対パス（`.` など）は見ない。カレントディレクトリに置かれた `mcrl-*` が勝手に動かないようにするため

## 作り方

最小の例は [examples/extensions/mcrl-hello](../../examples/extensions/mcrl-hello/mcrl-hello)。

```sh
#!/bin/sh
set -eu
exec "$MCRL_BIN" org get --jq '{greeting: "Hello, \(.name)!"}'
```

```sh
mcrl extension install ./examples/extensions/mcrl-hello   # 開発中はリンクで入れる
mcrl hello
```

作るときの約束:

- API は `"$MCRL_BIN"` 経由で呼ぶ。生の API が要るなら `"$MCRL_BIN" api /path`（`--paginate` や `-X POST -f file.json` も使える）
- 出力は mcrl に合わせて JSON にすると、`--jq` や他のコマンドとつなぎやすい。mcrl の出力を `"$MCRL_BIN" ... --jq` で整形して出すのが簡単
- エラーは stderr、終了コードは mcrl の表（`mcrl help exit-codes`）に合わせると扱いやすい。mcrl の終了コードをそのまま返してもよい
- `--help` を自分で実装する（mcrl は拡張のヘルプを作らない）
- 確認なしに更新系を実行しない。`--dry-run` や確認プロンプト（`MCRL_NO_INPUT` を尊重）を用意する

ヘルプ（`mcrl --help`）と補完には、入っている拡張が「Extension commands:」として本体のコマンドと分けて出る。拡張の引数の補完はしない。
