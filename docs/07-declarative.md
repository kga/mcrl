# 07. 宣言的管理（export / diff / apply）

ステータス: **構想** — スコープに含めるかは M4 の前に判断する（[decisions.md](decisions.md) D4）

## 狙い

監視・ダッシュボードなどの設定を YAML でリポジトリに置き、レビューを通してから反映したい。
Terraform を入れるほどではないチーム、あるいは Terraform 管理外の設定を手早く同期したい場面を想定する。

```sh
mcrl export monitor dashboard > mackerel/        # 現在の設定を書き出す
vim mackerel/monitors/cpu-high.yaml
mcrl diff -f mackerel/                           # 差分を見る
mcrl apply -f mackerel/                          # 反映（確認あり）
```

## 対象リソース

| リソース | 対象 | 識別キー（案） | 注意 |
|---|---|---|---|
| 監視 | ○ | `type` + `name` | connectivity は名前省略可なので要検討 |
| ダッシュボード | ○ | `urlPath` | API 上も一意 |
| ダウンタイム | ○ | `name` | |
| 通知チャンネル | △ | `name` | email / slack / webhook のみ |
| 通知グループ | ○ | `name` | デフォルトグループは改名不可 |
| アラートグループ設定 | ○ | `name` | |
| サービス・ロール | ○ | 名前 | 削除は連鎖削除を伴うため `--prune` でも既定で除外 |
| グラフ定義 | △ | `name` | 取得 API がないため export できない |
| AWS インテグレーション | △ | `name` | `secretKey` を扱えない |
| IP 制限 | △ | — | 自分を締め出す危険 |

## ファイル形式（案）

```yaml
# mackerel/monitors/cpu-high.yaml
kind: monitor
spec:
  type: host
  name: cpu high
  metric: cpu%
  operator: ">"
  warning: 80
  critical: 95
  duration: 5
  scopes: [web:app]
```

- `spec` は API の JSON そのまま（YAML 表記）。独自の DSL は作らない
- 1 ファイルに複数リソースを `---` 区切りで書いてもよい
- サーバー側の ID はファイルに書かない。識別キーで照合する

## 振る舞い

- `diff`: 識別キーで照合し、追加・変更・（`--prune` 時のみ）削除を表示。サーバー側にしかないフィールドは差分に出さない
- `apply`: `diff` を表示してから確認。`--yes` で省略
- `--prune` を付けない限り、ファイルにないリソースは消さない
- 識別キーが重複している場合（同名の監視が 2 つあるなど）は適用を拒否する
- 適用順は依存関係に従う（サービス → ロール → 通知チャンネル → 通知グループ → 監視 → …）
- 終了コード: `diff` は差分があれば 1、なければ 0（`--exit-code` 指定時。CI で「ずれ」を検出する用途）

## 未解決の論点

- [ ] そもそもスコープに含めるか（Terraform プロバイダとの棲み分け）
- [ ] 識別キーを名前にするか、ファイルに ID を書くか、ローカルの状態ファイルを持つか
- [ ] 一部だけ管理したい（チームごとに担当範囲が違う）場合の絞り込み方法（ラベル、名前の接頭辞、ディレクトリ単位）
- [ ] 監視の「名前変更」を「削除＋作成」と区別できない問題（アラート履歴が切れる）
