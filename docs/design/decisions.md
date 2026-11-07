# 決定事項と未決事項

設計上の判断を 1 か所で追うための台帳。決まったら「決定」に移し、日付と理由を残す。

## 決定

| # | 日付 | 決定 | 理由・詳細 |
|---|---|---|---|
| D1 | 2026-11-06 | 既存の Mackerel CLI（mkr）のソース・ドキュメントは参照しない | ゼロから設計するため。根拠は公式 API ドキュメントと一般のヘルプのみ |
| D2 | 2026-11-06 | 実装言語は **Go** | gojq による `--jq` 内蔵、cobra の動的補完、Mackerel 周辺ツールとの近さ。詳細は [02-language.md](02-language.md) |
| D3 | 2026-11-06 | ヘルプ・エラーメッセージは **英語のみ**。設計ドキュメントは日本語 | 実装が単純、エラー文で検索しやすい、AI エージェントにも扱いやすい。翻訳の維持コストを避ける |
| D4 | 2026-11-06 | 宣言的管理（export / diff / apply）は **M4 の前に改めて判断** する | M0〜M3 を優先。Terraform プロバイダとの棲み分けをそのとき検討。[07-declarative.md](07-declarative.md) |
| D5 | 2026-11-06 | API キーの環境変数は **`MACKEREL_APIKEY` のみ**。`MCRL_API_KEY` のような独自の変数は作らない | [06-auth-config.md](06-auth-config.md#api-キーの解決順) |
| D6 | 2026-11-06 | 出力は **JSON / JSONL のみ**。表・YAML・CSV などの人間向け形式は作らない | API との対応を 1 対 1 に保ち、機械可読性を優先する。人間は TTY での色付けと `--jq` で読む。ID を次のコマンドに渡す用途も `--jq '.[].id'` で賄う。[04-cli-ux.md](04-cli-ux.md#出力) |
| D7 | 2026-11-06 | ライセンスは **MIT** | `LICENSE` |
| D8 | 2026-11-06 | ディレクトリ単位の `.mcrl.toml` によるプロファイル固定は **後から入れる**（D24 で置き換え） | 最初のリリースには含めない。[06-auth-config.md](06-auth-config.md#プロファイルの選択順) |
| D9 | 2026-11-06 | Go は **最新の安定版とその 1 つ前** をサポートする | Go 本体のサポート方針と同じ |
| D10 | 2026-11-06 | モジュールパスは `github.com/kga/mcrl` | |
| D11 | 2026-11-06 | 新バージョン通知・自己更新は **作らない** | 更新はパッケージマネージャに任せる。頼まれていない通信を裏で発生させない（IP 制限環境・CI の邪魔になる）。[08-quality-release.md](08-quality-release.md#自己更新) |
| D12 | 2026-11-06 | `{"success": true}` しか返さない更新 API の後は、**リソースを取得し直して出力する**（`--no-refetch` で抑止） | 更新系コマンドの出力を「更新後のリソース」に揃え、パイプで次の処理に渡せるようにする。[04-cli-ux.md](04-cli-ux.md#json-出力の約束) |
| D13 | 2026-11-06 | GitHub Actions は **workflow lockfile（actions.lock）** で推移的な依存まで commit に固定し、CI で `gh actions-lock --verify` を実行する | タグの付け替えや乗っ取りで別のコードが実行されるのを防ぐ。lockfile の対象外（実行時に取得するツール）はバージョン固定で補う。[08-quality-release.md](08-quality-release.md#github-actions-の固定actionslock) |
| D14 | 2026-11-06 | CI は push・PR では **Linux のみ**。macOS・Windows はリリースのタグと手動実行のときだけ。Dependabot は月 1 回・1 本にまとめる | private リポジトリの Actions は月 $12 の無料枠で、macOS は $0.062/分と高い。1 回の CI が約 $0.23 から約 $0.04 に下がる。公開リポジトリにしたら見直す。[08-quality-release.md](08-quality-release.md#静的解析ci) |
| D15 | 2026-11-07 | v0.1.0 は **リポジトリを private のまま** 出し、配布は **GitHub Releases と ghcr.io/kga/mcrl のイメージ** だけにする。Release は下書きで作り、オーナーが確認して公開する | private では artifact attestation（Enterprise Cloud が必要）・Homebrew・aqua が使えないので外す。公開するときに見直す。[08-quality-release.md](08-quality-release.md#配布) |
| D16 | 2026-11-07 | リリースは **tagpr** で行う。main への push ごとにリリース PR が更新され、マージすると tagpr がタグを打ち、同じ workflow から macOS・Windows のテストと GoReleaser を呼ぶ | 版と変更履歴をリリース PR で確認してから出せる。GITHUB_TOKEN で打ったタグは別 workflow を起動しないため、呼び出し（workflow_call）でつなぐ。Release は GoReleaser が下書きで作る（tagpr の `release = false`）。[08-quality-release.md](08-quality-release.md#配布) |
| D17 | 2026-11-07 | Release を **workflow が自動で公開** する。承認はリリース PR のマージで行う。公開前にバイナリの版とアーカイブの数を確かめ、公開後に release attestation を確かめ、確かめられなければ workflow を失敗にする（リポジトリは private のまま。private でも attestation が作られることを v0.1.1 で確認）。検証は `attestations: read` を付けた `verify-release.yml` で行う | 手で公開する二度目の承認は不要（tagpr はリリース PR のマージを承認とみなす設計）。immutable releases のため、公開は資産がすべて揃った最後に行う。D15 の「オーナーが確認して公開」を置き換える。[08-quality-release.md](08-quality-release.md#配布) |
| D18 | 2026-11-07 | main はルールセットで守り、**PR 必須・必須チェック `ci-ok`・線形履歴**にする。マージは **squash のみ** で、PR を作ったら auto-merge を有効にする。tagpr は **GitHub App のトークン** を使い、リリースはタグで起動する `release.yml` に戻す | 一人でも PR と CI を必ず通す。承認は求めない（自分の PR は承認できない）。必須チェックを 1 つの集約ジョブにすると、文書だけの PR でも報告され止まらない。App のトークンならリリース PR でも CI が動き、タグで別 workflow も起動できるので、workflow_call の連鎖が要らなくなる。設定の一覧は [repository-settings.md](repository-settings.md) |
| D19 | 2026-11-07 | マージは **merge commit** にする（D18 の squash を置き換える）。ルールセットの線形履歴と署名必須は付けない | オーナーの好み。ブランチのコミットが main に残り、`git log --first-parent main` で PR 単位にも追える。merge commit は履歴が分岐するので線形履歴とは両立せず、ブランチのコミットに署名していないので署名必須も付けない。[repository-settings.md](repository-settings.md) |
| D20 | 2026-11-07 | `mcrl mcp`（stdio の MCP サーバー）は **ツール呼び出しを cobra コマンドの実行に変換** し、stdout の JSON を返す。SDK は公式の `github.com/modelcontextprotocol/go-sdk`（v1.8.0）。公開は 3 段階: 既定は **参照系だけ**、`--allow-write` で作成・更新系、**CLI で確認プロンプトを出すコマンド**（delete・host retire・user remove・invitation revoke・org ip-restriction set など）は `--allow-destructive` のときだけ公開し、そのときは `--yes` を付けて実行する | 共通関数に切り出す方式は 90 近いコマンドの RunE を書き換えることになり、CLI とずれる余地も残る。コマンドを実行すれば名前解決・検証・ページング・エラー分類が CLI と完全に同じになり、入力スキーマ（フラグと `Use` の位置引数から生成）と説明文（`Long`）も `--help` と同じ定義から作れる。呼び出しごとに新しいコマンドツリーと Factory を作るので並行呼び出しでも状態を共有しない。MCP では端末で確認を取れないため、「CLI が確認を求めるもの = 破壊的」を基準にサーバー起動時の明示的な許可を要求し、個々の呼び出しの確認はクライアントのツール承認（`destructiveHint`）に任せる。`--file` はサーバー側のファイルを読ませず `body` を stdin で渡す。エディタを開く・ローカルのコマンドを実行する・終わらない・認証情報を扱うコマンドは公開しない。[05-commands.md](05-commands.md#mcp-サーバー) |
| D21 | 2026-11-07 | API のエンドポイント一覧（101 本）を `internal/mackerel/endpoints.go` にデータで持ち、**各コマンドが使うエンドポイントはヘルプ（`withAPI` の「API:」行）から読み取って** 網羅をテストする（`TestEndpointCoverage`）。コマンドを作らないものは `endpointsLeftToAPI` に理由つきで書く。コマンドリファレンス `docs/reference/` は cobra のコマンドツリーから自前の生成器で作り（`mise run docs`）、生成し直した結果と一致することをテストで確かめる（`TestReferenceUpToDate`） | 対応表を手で書くとヘルプと二重管理になる。ヘルプの「API:」行を唯一の情報源にすれば、ヘルプ・網羅チェック・リファレンスが食い違わない。テストは未知のエンドポイントや、ヘルプの権限とエンドポイントが要る権限の食い違いも検出する。cobra/doc を使わないのは、`<id>` などが Markdown で消えないようヘルプをテキストブロックで出すため・トップレベルのコマンドごとに 1 ファイルにするため・依存を増やさないため。差分確認を Go のテストにしたのは `mise run test` でも気づけるため。[05-commands.md](05-commands.md)、[reference/endpoints.md](../reference/endpoints.md) |
| D22 | 2026-11-07 | `mcrl audit` を **参照だけの棚卸しコマンド** として足す。結果は `kind`・`severity`（`warning`/`info`）・`resource`・`message`・`details` を持つ「指摘」の配列。`--fail-on` で指摘があれば **終了コード 8**（既存の 0〜7・130 と重ならない新しい値）。各一覧は 1 回だけ取り、選んだチェックに要る一覧だけを取る | 設定は放っておくと腐る（忘れられたミュート、終わったダウンタイム、使われていない通知チャンネルなど）。cron や CI から定期的に回せるよう、機械で扱える形と終了コードにする。「エラー」と「指摘あり」を区別できるよう 1 とは別の値にした。ホストの API にはステータス変更時刻がないため、ホストの古さは `GET /tsdb/latest` の最新時刻で判断する。チェック監視にはホストとの対応がないため、退役ホストの残骸は未解決アラートで代用する。チェックの一覧と判定基準は [05-commands.md](05-commands.md#棚卸しaudit) |
| D23 | 2026-11-07 | **拡張** を入れる。`mcrl <name>` が本体のコマンド・別名でなければ、拡張ディレクトリ（`$MCRL_EXTENSIONS_DIR`、既定 `$XDG_DATA_HOME/mcrl/extensions`）→ `PATH` の順で `mcrl-<name>` を探し、引数・標準入出力・終了コードをそのまま受け渡して実行する。拡張には `MCRL_BIN`・`MCRL_PROFILE`・`MCRL_API_BASE` を渡し、**API キーは渡さない**。管理は `mcrl extension list/install/upgrade/remove`（install は git で clone してコミットを出力、ローカルのディレクトリはシンボリックリンク）。名前の前のグローバルフラグは環境変数で渡せる `--profile`・`--output`・`--color`・`--debug`・`--no-input` だけ許す | 本体に入れない機能（通知の避難訓練、しきい値の過去検証など）を外で作るため。05-commands の当初案（解決済みのキーを渡す）は、他人のコードと孫プロセスにキーが漏れやすくキーチェーンで保管する意味が薄れるので、本体を呼ばせる形に変えた。本体経由ならリトライやエラー分類もそのまま効く。本体の名前を常に優先し、`PATH` の相対エントリを見ないのは乗っ取り防止。更新は明示的な `upgrade` だけ（D11 と同じく裏で通信しない）で、`--dry-run` で先にコミットを確かめられる。分岐は cobra が引数を解釈する前に `internal/cli/extension.go` で行い、既存のコマンドには手を入れない。Ctrl-C はプロセスグループで拡張に届くので SIGINT は転送せず、SIGTERM・SIGHUP だけ転送する。リリースのバイナリを取ってくる機能と拡張の引数の補完は作らない。[extensions.md](extensions.md) |
| D24 | 2026-11-07 | ディレクトリ単位の `.mcrl.toml` は **作らない**（D8 を置き換える）。リポジトリごとにプロファイルを固定したいときは、mise・direnv などで環境変数 `MCRL_PROFILE` を設定する | mcrl はすでに `MCRL_PROFILE` を読み、`auth status` にどこで決まったかが出るので、ディレクトリ単位の環境変数を扱うツールに任せれば同じことができる。外のツールとは、mcrl が約束している口（環境変数・コマンド・拡張・JSON 出力と終了コード・MCP）だけでつなぎ、相手の設定ファイルを読んだり SDK を取り込んだりしない、という方針の一例。自前で親ディレクトリを辿ると「暗黙の設定読み込みはしない」原則（[01-vision.md](01-vision.md)）とぶつかり、どのファイルが効いたかを示す仕組みも要る。このリポジトリ自身も `mise.local.toml` で API キーを渡している。[06-auth-config.md](06-auth-config.md#ディレクトリごとにプロファイルを固定する) |
| D25 | 2026-11-07 | 文書を **利用者向け** と **設計資料** に分ける。`docs/` は利用者向け（**英語**、今の mcrl の動きだけを書く）、設計資料（日本語）は `docs/design/` に移す。コマンドリファレンス `docs/reference/` は利用者向けとしてそのまま。README は概要・インストール・最初の一歩と、`docs/` への入口に絞る | README が長くなり、秘密情報の管理ツールとの使い方のような利用者向けの説明の置き場がなかった。設計資料は経緯や未実装の構想を含み、利用者が読むと実際の動きと取り違える。利用者向けを英語にするのは、ヘルプ・エラーと同じ言語にそろえるため（D3）。`docs/` を利用者向けにしたのは、利用者が最初に開く場所だから。利用者向けの文書は、ヘルプ・実装・実 API で確かめたことだけを書き、構想は書かない |
| D26 | 2026-11-07 | `mcrl triage` を **参照だけの障害対応コマンド** として足す（設計は [10-triage.md](10-triage.md)）。開いているアラートを監視ごとにまとめ、重い状態 → 新しい順に並べ、上位 `--max-groups` 件だけ周辺情報（メトリクス・同じホストの他の監視・アノテーション・有効なダウンタイム）を取る。ログ・トレースは `--otel-service` で指定したサービスだけ。一部の取得の失敗は **終了コード 0** で `errors` に書く。割合監視は値を計算せず元のメトリクスを載せる | 障害の最初の数分に叩くコマンドを ID の受け渡しなしで 1 回にまとめるため。OpenTelemetry の `service.name` と Mackerel のサービス・ロールは別の概念で API に対応がないので、ログ・トレースは推測でたどらない。失敗した一部の情報のために他を捨てないよう、取れたものは返す（CI で使う用途はないので終了コードで知らせる必要は薄い）。判定は audit・`downtime list --active` と同じ関数（`hostByID`・`hostRetired`・`latestMetricTimes`・`downtimeActive`）、ログ・トレースのリクエストは `log search`・`trace list` と同じ組み立て（`logFlags.body`・`traceFlags.body`）を使い、コマンドの間で判定や条件がずれないようにした。割合は API に元の式が書かれておらずコア数の扱いなどで値がずれうるので計算しない。出力の細部（足したキー、`next` の中身、`*` を含むメトリクス名の扱い）は [10-triage.md](10-triage.md#実装で決めたこと) |

## 未決（優先度順）

- `mcrl triage` の残り（[10-triage.md](10-triage.md#未決) の T1・T3・T4）。T2・T5 は 2026-11-07 に解決し、実装済み（D26）。T4（ダウンタイム中にアラートが作られるか）は実 API で確認中

## 実 API で確認すること

- [x] エラーレスポンスの JSON 形式 → `{"error":{"message":"..."}}`（[api-notes.md](api-notes.md#エラー)）
- [ ] 429 が返るときのヘッダ（`Retry-After` が Logs 以外でも返るか）。通常時にレート制限系のヘッダがないことは確認済み
- [ ] `GET /hosts` に件数上限があるか（ホスト 1 台では確かめられない）
- [x] メトリクス取得の `from`/`to` → 秒、両方必須、400 日でも可。1 日以上の範囲では集約された点が返る（範囲ごとの正確な粒度は未確認）
- [x] ID の形式 → 11 文字の英数字（ホスト ID も同じ）
- [x] 退役したホストが一覧・取得でどう見えるか → 一覧と名前の検索には出ず、ID なら `isRetired: true` と `retiredAt` 付きで取れる。`status` は退役前のまま（[api-notes.md](api-notes.md#退役したホスト実測-2026-11-06)）
- [ ] alert-group-settings の単体取得・作成レスポンスのラップ有無（設定 0 件のため未確認）
