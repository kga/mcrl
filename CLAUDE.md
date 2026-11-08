# mcrl

Mackerel API のための新しい CLI。

- 設計資料（日本語）は private リポジトリ `kga/mcrl-design` にある。手元では `$(ghq root)/github.com/kga/mcrl-design`（入口は `README.md`、作業の約束は同じリポジトリの `CLAUDE.md`）。このリポジトリは public なので、コード・コメント・文書から設計資料を参照せず、理由はその場に書く
- 利用者向けの文書（英語）は `docs/`（入口は `docs/README.md`）。今の動きだけを書き、構想は書かない。コマンドの動きを変えたら合わせて直す
- コマンドリファレンス `docs/reference/` は `mise run docs` で生成する（手で編集しない）

## 厳守

- 既存の Mackerel CLI「mkr」のソースコード・ドキュメント・ヘルプページを **参照しない**。検索結果に出てきても開かない
- 根拠にしてよいのは https://mackerel.io/api-docs/ と Mackerel の一般ヘルプのみ

## ドキュメント

- 設計ドキュメントは日本語、利用者向けの文書・README・ヘルプは英語で書く
- 判断が決まったら mcrl-design の `decisions.md` に日付と理由を記録する
- 実 API で確かめた事実は mcrl-design の `api-notes.md` に反映する

## 開発の流れ

- `main` に直接 push しない（ルールセットで禁止）。変更ごとにブランチを切って PR を作る
  - ブランチ名は `feat/...`、`fix/...`、`ci/...`、`docs/...` など
  - マージは merge commit（`git log --first-parent main` で PR 単位に追える）。ブランチのコミットも main に残るので、1 コミットずつ意味のあるメッセージにする
  - PR のタイトルは英語の命令形にする。マージコミットのタイトルとリリースノートの 1 行になる
  - PR を作ったら `gh pr merge --auto --merge` で auto-merge を有効にする。必須チェック `ci-ok` が通ると GitHub がマージする
  - main が先に進んで衝突したら、PR のブランチを `git rebase origin/main` して `git push --force-with-lease` で更新する
  - リリース PR（`tagpr` ラベル）はマージした時点でリリースが公開されるので、auto-merge にせず、オーナーに確認してからマージする
  - PR には変更の種類のラベルを付ける。リリースノート（`.github/release.yml`）の分類と、tagpr の次の版の決定に使われる
    - `feature`: 機能追加（マイナーバージョンが上がる）
    - `breaking-change`: 互換性を壊す変更（メジャーバージョンが上がる）
    - ラベルなし: 修正・内部変更（パッチバージョン）
- リリースは tagpr で行う（mcrl-design の `08-quality-release.md`）。リリース PR をマージするとタグが打たれ、`release.yml` がテスト・ビルド・公開まで進める
- GitHub の設定は mcrl-design の `repository-settings.md` にまとめてある
- `.github/workflows/` を変えたら `gh actions-lock` で `actions.lock` を更新して同じ PR に含める。同じリポジトリの action・reusable workflow は `./` ではなく `$/` で呼ぶ
- 実 API での確認は検証用組織だけで行い、作ったリソースは片付ける。キーは mcrl のプロファイルで扱う: 参照は `mcrl-dev-ro`（`mise.local.toml` の `MCRL_PROFILE` でこのディレクトリの既定にしている）、更新系と擬似ホスト・`dev/otlp` は `mcrl-dev-rw`（`--profile mcrl-dev-rw`。`dev/` のスクリプトは `dev/write-key.sh` で自動的に使う）
