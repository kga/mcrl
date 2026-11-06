# リポジトリの設定

GitHub 側で、コードの外にある設定の一覧。workflow はこれらが揃っている前提で書いてある（[decisions.md](decisions.md) D18）。

## GitHub App（tagpr 用）

作成画面の Identifying and authorizing users（Callback URL・OAuth・Device Flow）と Post installation（Setup URL）は使わないので空のまま。

tagpr が `GITHUB_TOKEN` でリリース PR やタグを作ると、その PR で CI が動かず（必須チェックを満たせない）、タグで `release.yml` も起動しない。そのため tagpr には GitHub App のトークンを使う。

1. GitHub App を作る（Settings → Developer settings → GitHub Apps → New GitHub App）
   - Webhook: 無効
   - Repository permissions: **Contents: Read and write**、**Pull requests: Read and write**、**Issues: Read-only**（Metadata: Read-only は自動）
   - Where can this GitHub App be installed: Only on this account
2. 秘密鍵（private key）を生成してダウンロードする
3. App を `kga/mcrl` だけにインストールする
4. Environment `tagpr` を作る（Settings → Environments → New environment）
   - Deployment branches and tags: **Selected branches and tags** → `main` だけ
   - Required reviewers: なし（付けると main への push のたびに承認待ちになる）
   - リポジトリの secret ではなく Environment に置くのは、同じリポジトリのブランチから作った PR の workflow にはリポジトリの secret が渡り、ブランチ上で workflow を書き換えれば鍵を読めてしまうため。Environment を `main` に限れば、main で動くジョブからしか読めない
5. Environment `tagpr` に登録する（鍵がクリップボードを通らないよう `gh` で入れる）

   ```sh
   gh secret set TAGPR_APP_PRIVATE_KEY --env tagpr --repo kga/mcrl < path/to/private-key.pem
   gh variable set TAGPR_APP_CLIENT_ID --env tagpr --repo kga/mcrl --body '<Client ID>'
   ```

   登録したら .pem は消してよい（App のページで作り直せる）

## ブランチのルールセット（main）

Settings → Rules → Rulesets の `main` 用ルールセット（既存の Restrict deletions・Block force pushes に足す）:

| ルール | 設定 | 理由 |
|---|---|---|
| Target branches | Default branch | |
| Bypass list | 空 | オーナー（Claude もオーナーのトークンで動く）も含めて例外を作らない |
| Restrict deletions | ✓ | |
| Block force pushes | ✓ | main の履歴を書き換えない（PR のブランチは対象外） |
| Require linear history | ✗ | マージは merge commit で行う（[decisions.md](decisions.md) D19）。merge commit は履歴を分岐させるので付けられない |
| Require a pull request before merging | ✓、Required approvals **0** | 一人なので承認は求めず、PR を通すことだけを強制する |
| Require status checks to pass | ✓、チェック **`ci-ok`**（GitHub Actions） | CI の結果を 1 つに集約したジョブ。変更が文書だけなら重いジョブを飛ばして成功する |
| 　Require branches to be up to date | ✗ | Dependabot や tagpr の PR が main に遅れるたびに止まるのを避ける |
| Require signed commits | ✗ | merge commit ではブランチのコミットも main に入る。ブランチのコミットに署名していないので付けない |

## General（Settings → General → Pull Requests）

| 項目 | 設定 |
|---|---|
| Allow merge commits | ✓、Default commit message: **Pull request title**（既定の `Merge pull request #N from ...` より何の変更かが読める） |
| Allow squash merging | ✗ |
| Allow rebase merging | ✗ |
| Always suggest updating pull request branches | ✓ |
| Allow auto-merge | ✓ |
| Automatically delete head branches | ✓ |

## Actions（Settings → Actions → General）

| 項目 | 設定 |
|---|---|
| Workflow permissions | Read repository contents（既定を読み取りのみにし、各 workflow で必要な権限だけ付ける） |
| Allow GitHub Actions to create and approve pull requests | ✗ に戻してよい（tagpr は App のトークンを使う） |
| Require lockfile（Actions のポリシー） | Active、All workflows |

## Releases・Packages

- Settings → General → Releases: **Enable release immutability** ✓
- パッケージ `ghcr.io/kga/mcrl` は private（リポジトリを公開するときに合わせて公開する）
