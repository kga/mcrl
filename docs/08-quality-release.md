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
- GitHub Actions: Linux / macOS / Windows でテスト
- ツールのバージョンは `mise.toml` で固定し、ローカルと CI で同じものを使う

## 配布

GoReleaser で以下を一度に作る。

| 経路 | 対象 |
|---|---|
| GitHub Releases | tar.gz / zip（darwin・linux・windows × amd64・arm64）、チェックサム |
| Homebrew | tap（`kga/tap/mcrl`） |
| aqua / mise | aqua registry への登録（mise からも `aqua:` バックエンドで入る） |
| Linux パッケージ | deb / rpm / apk（nfpm） |
| コンテナイメージ | `ghcr.io/kga/mcrl`（distroless、マルチアーキ） |
| Windows | Scoop / WinGet |
| Go | `go install github.com/kga/mcrl/cmd/mcrl@latest` |

### サプライチェーン

- 成果物に cosign（keyless）で署名
- SBOM（SPDX）を添付
- GitHub の artifact attestation で SLSA provenance を付ける
- 依存の更新は Renovate、`govulncheck` を CI で実行

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
