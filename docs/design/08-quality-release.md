# 08. 品質・配布・互換性

ステータス: **提案（レビュー待ち）**

## テスト

| 層 | 方法 | 内容 |
|---|---|---|
| 単体 | `go test` | 時刻パーサ、名前解決、ページネーション、エラー変換、出力整形 |
| API クライアント | `httptest.Server` ＋記録したレスポンス（`testdata/fixtures/`） | リクエストの method・path・body の検証、リトライ・レート制限の挙動 |
| CLI | `testscript`（`testdata/script/*.txtar`） | コマンドライン → stdout / stderr / 終了コードを 1 ファイルで記述。偽 API サーバーを起動して実行 |
| 出力 | ゴールデンファイル（`-update` で更新） | JSON 出力・エラー出力が意図せず変わっていないか |
| 実 API | ビルドタグ `e2e`、テスト用組織の API キーを環境変数で渡す | 手動または定期実行のみ。作ったリソースは必ず後始末 |
| 網羅性 | エンドポイント定義表（[03-architecture.md](03-architecture.md#エンドポイント定義はデータで持つ)）と CLI の対応 | 全エンドポイントにコマンドがあることを CI で確認 |

`testscript` の例:

```
# host retire は 30 台ずつに分割される
exec mcrl host retire --yes h1 h2 ... h31
stdout 'retired 31 hosts'
api-calls 'POST /api/v0/hosts/bulk-retire' 2
```

## 静的解析・CI

- `golangci-lint`（`errcheck`、`govet`、`staticcheck`、`revive` など）
- `go vet`、`go test -race`
- GitHub Actions（private リポジトリでは無料枠が月 $12 ぶんで、macOS は Linux の約 10 倍の料金のため、回数と OS を絞る。[decisions.md](decisions.md) D14）
  - push・PR: Linux で Go 2 バージョンのテスト、lint、govulncheck、actions.lock の検証。結果は `ci-ok` ジョブに集約し、これを main の必須チェックにする。ドキュメント・`dev/`・`LICENSE` だけの PR では重いジョブを飛ばす（workflow 自体は起動して `ci-ok` を報告する）。同じブランチの古い実行は取り消す
  - macOS・Windows: `cross-platform.yml` で、リリースのタグと手動実行（`gh workflow run cross-platform.yml`）のときだけ
  - Dependabot: エコシステムごとに 1 本の PR にまとめて月 1 回
- ツールのバージョンは `mise.toml` で固定し、ローカルと CI で同じものを使う

## 配布

GoReleaser で作る（[decisions.md](decisions.md) D15。リポジトリが private の間の構成）。

| 経路 | 内容 | 状態 |
|---|---|---|
| GitHub Releases | tar.gz / zip（darwin・linux・windows × amd64・arm64）、`checksums.txt`。各アーカイブに `LICENSE`・`THIRD_PARTY_LICENSES`・`README.md`・`completions/`（bash・zsh・fish） | v0.1.0 から。`THIRD_PARTY_LICENSES` は v0.3.0 から |
| コンテナイメージ | `ghcr.io/kga/mcrl:<version>` と `latest`（linux/amd64・arm64）。distroless static（非 root）をダイジェストで固定。`/usr/share/doc/mcrl/` に `LICENSE`・`THIRD_PARTY_LICENSES` | v0.1.0 から。ライセンス表示は v0.3.0 から |
| Go | `go install github.com/kga/mcrl/cmd/mcrl@latest` | リポジトリを公開したら |
| Homebrew / aqua / Scoop / WinGet / deb・rpm | | リポジトリを公開するときに検討 |

### ライセンス表示

mcrl のバイナリには、依存モジュール（MIT・BSD・Apache-2.0）と Go の標準ライブラリ・ランタイム（BSD）が入る。これらはバイナリで配布するときにもライセンス本文と著作権表示を添えることを求めるので、リリースのたびに `scripts/third-party-licenses.sh`（GoReleaser の before フック）が、リリースする全 OS の依存のライセンス（と NOTICE があればそれも）を `THIRD_PARTY_LICENSES` にまとめ、アーカイブとイメージに入れる。v0.2.2 までの配布物には入っていなかった（2026-11-08 に気づいて v0.3.0 から入れた）。

リリースの手順（[tagpr](https://github.com/Songmu/tagpr)、[decisions.md](decisions.md) D16）:

1. main に push すると、tagpr が「リリース PR」（次の版と `CHANGELOG.md`）を作る・更新する
2. 次の版はリリース PR のラベルで決まる: `tagpr:minor` / `tagpr:major`、なければ patch。マージ済み PR に `feature`・`minor` / `major`・`breaking-change` ラベルがあれば tagpr が自動で付ける
3. **リリース PR のマージがリリースの承認**。マージすると tagpr がタグを打つ。tagpr は GitHub App のトークンを使うので、このタグで `release.yml` が起動する（リリース PR でも CI が動く）
4. `release.yml` は、`cross-platform.yml`（macOS・Windows のテスト）を通したあと、GoReleaser で Release の下書きと ghcr のイメージを作り、バイナリの版とアーカイブの数を確かめてから **自動で公開** し、`verify-release.yml` で release attestation を確かめる（確かめられなければ失敗）。手で公開する作業はない
5. 途中で失敗したら、`gh workflow run release.yml -f tag=vX.Y.Z` でそのタグのリリースをやり直せる（下書きは使い回す）。公開まで済んで検証だけ失敗したときは、公開済みの Release は変えられないので `gh workflow run verify-release.yml -f tag=vX.Y.Z` で検証だけやり直す

- リポジトリは **immutable releases** を有効にしている（2026-11-07）。公開した Release は、資産もタグも変更・削除できない。そのため GoReleaser は下書きを作ってすべての資産を付け、検査が通ってから workflow が公開する（[decisions.md](decisions.md) D17）。workflow を再実行したときは同じタグの下書きを使い回し（`use_existing_draft`）、途中まで上げた資産は置き換える（`replace_existing_artifacts`）。公開すると GitHub が release attestation を作る。private リポジトリでも作られることを v0.1.1 で確認した（2026-11-07）。workflow の `GITHUB_TOKEN` で読むには **`attestations: read`** が要る（ないと `gh release verify` が「no attestations」になる。v0.1.1・v0.2.0 の CI での検証はこれで失敗していた）。利用者は `gh release verify <tag>`、ダウンロードしたファイルは `gh release verify-asset <tag> <file>` で確かめられる。Web UI でも、Release のページの Assets に各ファイルの sha256 と **Release attestation (json)** が並ぶ（v0.1.0 は有効化の前に公開したので対象外）
- 変更履歴は GitHub が生成するリリースノート（マージされた PR が単位、分類は `.github/release.yml`）なので、main に直接 push した変更は載らない
- `gh workflow run release.yml`（tag なし）でスナップショット（公開しない）を作れる。人が手で `v*` タグを push した場合も同じ流れで動く
- 前提となる GitHub の設定（App、ルールセット、auto-merge など）は [repository-settings.md](repository-settings.md)

### サプライチェーン

- 成果物に GitHub の artifact attestation で SLSA provenance を付けるのは、リポジトリを公開してから（private では Enterprise Cloud が必要）
- 成果物への cosign（keyless）署名と SBOM（SPDX）の添付は配布準備のときに入れる（SBOM の生成には CI に syft が要る）
- Go の依存と GitHub Actions の更新は Dependabot（`cooldown` 7 日。公開直後の版をすぐには取り込まない）
- `govulncheck` を CI で実行（バージョン固定）

### GitHub Actions の固定（actions.lock）

[workflow lockfile](https://docs.github.com/actions/concepts/security/workflow-dependency-lockfiles)（2026-10-28 GA）で、workflow が使う action を推移的な依存まで commit に固定する（[decisions.md](decisions.md) D13）。

- `.github/workflows/actions.lock` は `gh actions-lock`（`github/gh-actions-lock` 拡張）で生成する。手で編集しない
- workflow を変えたら `gh actions-lock` を実行して lockfile を更新し、同じコミットに含める
- CI の `actions-lock` ジョブが `gh actions-lock --verify` で lockfile の不足・ずれを検出する（拡張自体も `--pin` で版を固定）
- Dependabot が action を更新するときは lockfile も同じ PR で更新される
- `uses:` はタグ（完全な semver）で書く。lockfile があれば `uses:` に SHA を書く必要はない
- 同じリポジトリの composite action・reusable workflow は `./` ではなく `$/` で呼ぶ（ランナーに checkout された内容ではなく呼び出し元と同じコミットのものが使われ、呼び出し元の lockfile が適用される。[参考](https://songmu.jp/riji/entry/2026-10-30-github-actions-lockfile.html)）。他リポジトリの reusable workflow（`jobs.<id>.uses`）は lockfile の対象外なので、使う場合は SHA で固定する
- workflow 内で実行時に取ってくるツール（golangci-lint、goreleaser、govulncheck、gh 拡張）は lockfile の対象外なので、すべてバージョンを固定する
- その他: `permissions` は既定 `contents: read` でジョブごとに最小限、`actions/checkout` は `persist-credentials: false`、`.github/` は CODEOWNERS でオーナーを担当にしている（一人で開発しているので、ルールセットではレビューと承認を必須にしていない。[repository-settings.md](repository-settings.md)）
- リポジトリ設定で「Require lockfile」（Actions のポリシー）を有効にすると、lockfile に載っていない workflow の実行を止められる。設定の変更はオーナーが行う

### 自己更新

`mcrl update` のような自己更新も、新しいバージョンがあることの通知も **作らない**（[decisions.md](decisions.md) D11）。

- 更新はパッケージマネージャに任せる
- mcrl が通信するのは、利用者が実行したコマンドに必要な Mackerel API だけにする

## バージョニングと互換性

- SemVer。1.0.0 までは破壊的変更がありうる
- 1.0.0 以降、**以下を公開インターフェースとして互換性を保つ**:
  - コマンド名・フラグ名
  - JSON / JSONL 出力の構造（API がフィールドを追加した場合の追加は互換）
  - 終了コード
  - 環境変数名・設定ファイルのキー
- TTY でのインデント・色付けは見た目の問題なので互換性の対象外
- 廃止するフラグは 1 マイナーバージョン以上、警告を出してから消す

## セキュリティ

- API キーは stdout / stderr / デバッグログ / エラーメッセージに出さない（`auth token` を除く）
- `api_key_command` の実行時はシェルを介さず、引数を分割して実行する（設定は自分で書くものなので過度な制限はしない）
- 一時ファイル（`edit` 用）は 0600 で作り、終了時に削除
- AWS インテグレーションの `secretKey` は出力・編集画面に出さない
