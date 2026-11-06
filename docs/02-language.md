# 02. 実装言語の選定

ステータス: **決定（2026-11-06）** — **Go**（[decisions.md](decisions.md) D2）

## 要件

[01-vision.md](01-vision.md) の原則から、言語に求めるものを洗い出す。

| # | 要件 | 重み |
|---|---|---|
| R1 | 単一バイナリ・ランタイム依存なしで配布できる | 必須 |
| R2 | macOS / Linux / Windows × amd64 / arm64 へ簡単にクロスビルドできる | 必須 |
| R3 | 起動が速い（目標 50ms 以内） | 高 |
| R4 | jq 互換のフィルタを組み込める | 高 |
| R5 | シェル補完（静的＋API を引く動的補完）を作りやすい | 高 |
| R6 | OS のキーチェーンを使える | 高 |
| R7 | 多態な JSON（監視種別・ウィジェット種別などのタグ付き共用体）を扱いやすい | 中 |
| R8 | 対話プロンプト・TUI 部品のライブラリが充実している | 低 |
| R9 | Mackerel 利用者層がコントリビュートしやすい | 中 |
| R10 | 言語・ライブラリが長期的に後方互換を保つ | 中 |

## 候補と評価

候補は Go、Rust、TypeScript（Bun / Deno の単一バイナリ化）。
Zig・Nim・Swift などはライブラリの層が薄く、R4〜R8 を自前で作る量が多すぎるため除外した。

| 要件 | Go | Rust | TypeScript (Bun/Deno compile) |
|---|---|---|---|
| R1 単一バイナリ | ◎ 標準 | ◎ 標準 | △ ランタイム同梱で 50–100MB |
| R2 クロスビルド | ◎ `GOOS`/`GOARCH` だけ。CGO 不要なら完結 | ○ cargo-zigbuild 等が要る。macOS 向けは手間 | ○ `--target` 指定で可 |
| R3 起動速度 | ◎ 〜10ms | ◎ 〜数 ms | △ 30–80ms 程度 |
| R4 jq 内蔵 | ◎ **gojq**（jq とほぼ完全互換、純 Go、gh CLI でも採用） | ○ jaq（高速だが jq と細部の互換差あり） | △ WASM の jq か自前 |
| R5 補完 | ◎ cobra が bash/zsh/fish/PowerShell の動的補完まで標準装備 | ◎ clap_complete（動的補完は発展途上） | ○ 選択肢が分散 |
| R6 キーチェーン | ○ go-keyring（CGO 不要） | ○ keyring crate | △ ネイティブ拡張が要る |
| R7 タグ付き共用体 | △ 言語機能なし。`UnmarshalJSON` を手書き | ◎ serde の `#[serde(tag = "type")]` で宣言的 | ○ 判別共用体＋zod |
| R8 TUI・装飾 | ◎ Charm 系（bubbletea / lipgloss / huh） | ◎ ratatui | ○ ink など |
| R9 コントリビュータ | ◎ Mackerel 周辺（agent・プラグイン群）が Go 中心 | ○ | ○ |
| R10 後方互換 | ◎ Go 1 互換性保証 | ○ edition 制で安定 | △ ランタイム・ツールの変化が速い |

## 判断

**Go を推奨する。**

決め手は 3 つ。

1. **gojq を組み込める**。`--jq` を内蔵するとスクリプト利用時に jq の別途インストールが要らない。jq との互換性の高さは他言語の実装より一段上
2. **cobra の動的補完**。`mcrl host get <TAB>` でホスト名を API から引いて補完する、といった体験を少ない実装量で作れる
3. **Mackerel 利用者との距離**。mackerel-agent やチェックプラグイン群が Go で書かれており、利用者がそのままコントリビュートできる

### Go の弱点と対策

| 弱点 | 対策 |
|---|---|
| タグ付き共用体がない（監視 7 種、ウィジェット 4 種、グラフ定義 6 種など） | 型付き構造体は **名前解決・フィルタ用の読み取りモデル** に限定する。更新系は生 JSON（`map[string]any` / `json.RawMessage`）を基本にし、型の網羅性に頼らない設計にする（[03-architecture.md](03-architecture.md)） |
| エラー処理が冗長 | API エラーを 1 つの型に集約し、終了コードへの変換を 1 箇所に寄せる |
| バイナリサイズ（〜15MB） | `-ldflags="-s -w"` と `-trimpath`。CLI としては許容範囲 |

### Rust を選ぶなら

型の表現力（R7）と起動速度を最重視するなら Rust も十分ありうる。
その場合は clap + serde + reqwest（blocking）+ jaq + ratatui の構成になり、jq 互換性の細部と開発速度で妥協することになる。

## 確認したいこと

- [x] Go で確定してよいか → 確定
- [x] 最低サポートする Go バージョン → 最新の安定版とその 1 つ前（[decisions.md](decisions.md) D9）
