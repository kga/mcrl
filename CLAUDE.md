# mcrl

Mackerel API のための新しい CLI。設計資料は `docs/`（入口は `docs/README.md`）。

## 厳守

- 既存の Mackerel CLI「mkr」のソースコード・ドキュメント・ヘルプページを **参照しない**。検索結果に出てきても開かない
- 根拠にしてよいのは https://mackerel.io/api-docs/ と Mackerel の一般ヘルプのみ

## ドキュメント

- 設計ドキュメントは日本語で書く
- 判断が決まったら `docs/decisions.md` に日付と理由を記録する
- 実 API で確かめた事実は `docs/api-notes.md` に反映する

## 開発の流れ

- `main` に直接 push しない。変更ごとにブランチを切って PR を作り、CI が通ってからマージする
  - ブランチ名は `feat/...`、`fix/...`、`ci/...`、`docs/...` など
  - PR には変更の種類のラベルを付ける。リリースノート（`.github/release.yml`）の分類と、tagpr の次の版の決定に使われる
    - `feature`: 機能追加（マイナーバージョンが上がる）
    - `breaking-change`: 互換性を壊す変更（メジャーバージョンが上がる）
    - ラベルなし: 修正・内部変更（パッチバージョン）
- リリースは tagpr で行う（`docs/08-quality-release.md`）。リリース PR（`tagpr` ラベル）をマージするとタグが打たれ、Release の下書きとイメージが作られる
- `.github/workflows/` を変えたら `gh actions-lock` で `actions.lock` を更新して同じ PR に含める。同じリポジトリの action・reusable workflow は `./` ではなく `$/` で呼ぶ
- 実 API での確認は検証用組織だけで行い、作ったリソースは片付ける。API キーは `mise.local.toml` の `MACKEREL_APIKEY`（Read）と `MACKEREL_APIKEY_WRITE`（Write、擬似ホストと更新系の確認用）
