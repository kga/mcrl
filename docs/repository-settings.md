# リポジトリの設定

GitHub と Claude Code 側で、コードの外にある設定の一覧。workflow はこれらが揃っている前提で書いてある（[decisions.md](decisions.md) D18）。

## GitHub App（tagpr 用）

tagpr が `GITHUB_TOKEN` でリリース PR やタグを作ると、その PR で CI が動かず（必須チェックを満たせない）、タグで `release.yml` も起動しない。そのため tagpr には GitHub App のトークンを使う。

1. GitHub App を作る（Settings → Developer settings → GitHub Apps → New GitHub App）
   - Webhook: 無効
   - Repository permissions: **Contents: Read and write**、**Pull requests: Read and write**、**Issues: Read-only**（Metadata: Read-only は自動）
   - Where can this GitHub App be installed: Only on this account
2. 秘密鍵（private key）を生成してダウンロードする
3. App を `kga/mcrl` だけにインストールする
4. `kga/mcrl` の Settings → Secrets and variables → Actions に登録する
   - Variables: `TAGPR_APP_CLIENT_ID` = App の Client ID
   - Secrets: `TAGPR_APP_PRIVATE_KEY` = 秘密鍵（.pem の中身）

## ブランチのルールセット（main）

Settings → Rules → Rulesets の `main` 用ルールセット（既存の Restrict deletions・Block force pushes に足す）:

| ルール | 設定 | 理由 |
|---|---|---|
| Target branches | Default branch | |
| Bypass list | 空 | オーナー（Claude もオーナーのトークンで動く）も含めて例外を作らない |
| Restrict deletions | ✓ | |
| Block force pushes | ✓ | main の履歴を書き換えない（PR のブランチは対象外） |
| Require linear history | ✓ | squash マージだけにするので、main は 1 PR = 1 コミット |
| Require a pull request before merging | ✓、Required approvals **0** | 一人なので承認は求めず、PR を通すことだけを強制する |
| Require status checks to pass | ✓、チェック **`ci-ok`**（GitHub Actions） | CI の結果を 1 つに集約したジョブ。変更が文書だけなら重いジョブを飛ばして成功する |
| 　Require branches to be up to date | ✗ | Dependabot や tagpr の PR が main に遅れるたびに止まるのを避ける。squash + 線形履歴で十分 |
| Require signed commits | ✓（任意） | squash マージのコミットは GitHub が署名するので満たせる |

## General（Settings → General → Pull Requests）

| 項目 | 設定 |
|---|---|
| Allow merge commits | ✗ |
| Allow squash merging | ✓、Default commit message: **Pull request title and description** |
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

## Claude Code（このリポジトリでの許可）

PR のブランチを最新の main に rebase して push し直すため、`.claude/settings.local.json`（コミットしない）に許可を足す:

```json
{
  "permissions": {
    "allow": ["Bash(git push --force-with-lease:*)"]
  }
}
```

main への force push はルールセットで拒否されるので、この許可で main が書き換わることはない。
