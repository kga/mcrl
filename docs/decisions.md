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
| D8 | 2026-11-06 | ディレクトリ単位の `.mcrl.toml` によるプロファイル固定は **後から入れる** | 最初のリリースには含めない。[06-auth-config.md](06-auth-config.md#プロファイルの選択順) |
| D9 | 2026-11-06 | Go は **最新の安定版とその 1 つ前** をサポートする | Go 本体のサポート方針と同じ |
| D10 | 2026-11-06 | モジュールパスは `github.com/kga/mcrl` | |
| D11 | 2026-11-06 | 新バージョン通知・自己更新は **作らない** | 更新はパッケージマネージャに任せる。頼まれていない通信を裏で発生させない（IP 制限環境・CI の邪魔になる）。[08-quality-release.md](08-quality-release.md#自己更新) |
| D12 | 2026-11-06 | `{"success": true}` しか返さない更新 API の後は、**リソースを取得し直して出力する**（`--no-refetch` で抑止） | 更新系コマンドの出力を「更新後のリソース」に揃え、パイプで次の処理に渡せるようにする。[04-cli-ux.md](04-cli-ux.md#json-出力の約束) |
| D13 | 2026-11-06 | GitHub Actions は **workflow lockfile（actions.lock）** で推移的な依存まで commit に固定し、CI で `gh actions-lock --verify` を実行する | タグの付け替えや乗っ取りで別のコードが実行されるのを防ぐ。lockfile の対象外（実行時に取得するツール）はバージョン固定で補う。[08-quality-release.md](08-quality-release.md#github-actions-の固定actionslock) |
| D14 | 2026-11-06 | CI は push・PR では **Linux のみ**。macOS・Windows はリリースのタグと手動実行のときだけ。Dependabot は月 1 回・1 本にまとめる | private リポジトリの Actions は月 $12 の無料枠で、macOS は $0.062/分と高い。1 回の CI が約 $0.23 から約 $0.04 に下がる。公開リポジトリにしたら見直す。[08-quality-release.md](08-quality-release.md#静的解析ci) |
| D15 | 2026-11-07 | v0.1.0 は **リポジトリを private のまま** 出し、配布は **GitHub Releases と ghcr.io/kga/mcrl のイメージ** だけにする。Release は下書きで作り、オーナーが確認して公開する | private では artifact attestation（Enterprise Cloud が必要）・Homebrew・aqua が使えないので外す。公開するときに見直す。[08-quality-release.md](08-quality-release.md#配布) |
| D16 | 2026-11-07 | リリースは **tagpr** で行う。main への push ごとにリリース PR が更新され、マージすると tagpr がタグを打ち、同じ workflow から macOS・Windows のテストと GoReleaser を呼ぶ | 版と変更履歴をリリース PR で確認してから出せる。GITHUB_TOKEN で打ったタグは別 workflow を起動しないため、呼び出し（workflow_call）でつなぐ。Release は GoReleaser が下書きで作る（tagpr の `release = false`）。[08-quality-release.md](08-quality-release.md#配布) |
| D17 | 2026-11-07 | リポジトリを public にし、Release を **workflow が自動で公開** する。承認はリリース PR のマージで行う。公開前にバイナリの版とアーカイブの数を確かめ、artifact attestation を付け、公開後に release attestation を検証する | 手で公開する二度目の承認は不要（tagpr はリリース PR のマージを承認とみなす設計）。immutable releases のため、公開は資産がすべて揃った最後に行う。public になったので attestation を付け直す。D15 の「private のまま」「オーナーが確認して公開」を置き換える。[08-quality-release.md](08-quality-release.md#配布) |

## 未決（優先度順）

なし。

## 実 API で確認すること

- [x] エラーレスポンスの JSON 形式 → `{"error":{"message":"..."}}`（[api-notes.md](api-notes.md#エラー)）
- [ ] 429 が返るときのヘッダ（`Retry-After` が Logs 以外でも返るか）。通常時にレート制限系のヘッダがないことは確認済み
- [ ] `GET /hosts` に件数上限があるか（ホスト 1 台では確かめられない）
- [x] メトリクス取得の `from`/`to` → 秒、両方必須、400 日でも可。1 日以上の範囲では集約された点が返る（範囲ごとの正確な粒度は未確認）
- [x] ID の形式 → 11 文字の英数字（ホスト ID も同じ）
- [ ] 退役したホストが一覧・取得でどう見えるか
- [ ] alert-group-settings の単体取得・作成レスポンスのラップ有無（設定 0 件のため未確認）
