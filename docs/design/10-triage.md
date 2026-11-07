# 10. 障害対応の最初の 1 コマンド（triage）

ステータス: **提案（レビュー待ち）** — [09-roadmap.md](09-roadmap.md) M5 の候補

## 狙い

アラートが鳴ったとき、最初の数分でやることはだいたい決まっている。どの監視が・どのホストで・いつから鳴っているか、同じホストで他に何が起きているか、直前にデプロイはなかったか、ログやトレースにエラーは出ているか。今の mcrl でもすべて調べられるが、`alert list --expand`・`host monitor-status`・`metric get`・`annotation list`・`log search`・`trace list` を、ID を引き継ぎながら順に叩く必要がある。

`mcrl triage` は、これを **1 回の参照だけのコマンド** にまとめる。人が端末で読むほか、MCP のツールとして AI エージェントが障害対応の最初に呼ぶことを想定する。

- 何も変更しない。アラートを閉じる・メモを書くといった操作は、出力に載せた「次に叩くコマンド」を人やエージェントが選んで実行する
- 原因の推定や要約の文章は作らない。事実（取得した値と、決まった規則で付ける注記）だけを返す。解釈は人とエージェントに任せる
- 一部の取得に失敗しても（ログの権限がない、など）、取れたものは返す
- ログ・トレースは、指定された OpenTelemetry のサービスについてだけ取る。アラートからたどる手がかりがないため（[ログ・トレース](#ログトレース)）

## audit との違い

| | `audit` | `triage` |
|---|---|---|
| 問い | 設定に放置や古いものが残っていないか | いま何が起きていて、どこから調べればいいか |
| 対象 | 設定（監視・ダウンタイム・通知・ユーザーなど） | 状態（開いているアラートと、その周辺） |
| 時間 | 数日〜数か月単位 | アラートの前後数十分 |
| 使い方 | cron や CI で定期的に回す | 障害が起きたときに手やエージェントで叩く |
| 結果 | 「指摘」の配列。`--fail-on` で終了コード 8 | 監視ごとの束。合否は出さない（終了コード 0） |

判定の一部は共通にする。たとえば「監視がミュート中」「ホストが退役済み」「ホストのメトリクスが途絶えている」は audit のチェックと同じ関数で判定し、triage では関係するアラートに注記として添える。

## 使い方

```sh
# 開いているアラートすべて（監視ごとにまとめ、重い順に最大 5 件を詳しく）
mcrl triage

# 1 件のアラートを詳しく
mcrl triage 5VhFXR2a8mC

# サービスやホストで絞る
mcrl triage --service web
mcrl triage --host web-3 --window 30m

# OpenTelemetry のサービス（service.name）のエラーログ・トレースも並べる
mcrl triage --otel-service checkout-api --otel-service payment

# 人が読む用に絞る
mcrl triage --jq '.groups[] | [.status, .monitor.name, (.alerts | length)] | @tsv'
```

フラグ:

| フラグ | 既定 | 意味 |
|---|---|---|
| `[<alert>...]` | なし | アラート ID（`-` で stdin）。指定すると閉じたアラートも対象にできる |
| `--service` / `--role` / `--host` / `--monitor` | なし | 対象のアラートを絞る（名前で指定できる） |
| `--status` | 全部 | `CRITICAL`・`WARNING`・`UNKNOWN`（繰り返し・カンマ区切り可） |
| `--window` | `15m` | アラートの開始時刻の前後どれだけを周辺情報として取るか |
| `--max-groups` | `5` | 周辺情報まで取る監視の数。残りはアラートの一覧だけ載せる |
| `--max-hosts` | `3` | 1 つの監視について、メトリクスや他の監視の状態を取るホストの数 |
| `--max-samples` | `5` | ログ・トレースの見本の件数（サービスごと） |
| `--skip` / `--only` | なし | 取る情報を選ぶ: `metrics`・`monitors`・`annotations`・`downtimes`・`logs`・`traces` |
| `--otel-service` | なし | ログ・トレースを取る OpenTelemetry のサービス（`service.name`。繰り返し・カンマ区切り可）。指定しなければログ・トレースは取らない |

## まとめ方

アラートは **監視ごとの「グループ」** にまとめる。1 つのディスク監視が 50 台で鳴っているとき、50 件を別々に並べても読めないし、周辺情報を 50 回取るのも無駄なため。

- グループの並びは、最も重い状態（`CRITICAL` → `WARNING` → `UNKNOWN`）、次に最も新しい開始時刻の順
- 周辺情報は上位 `--max-groups` 件のグループについてだけ取る。グループの中のホストは、開始時刻が新しい順に `--max-hosts` 台まで
- 周辺情報を取る時間の範囲は、グループで最も早い開始時刻を `t` として `[t - window, min(t + window, 今)]`。既定の 15 分なら最大 30 分で、トレース検索の 30 分の上限（[api-notes.md](api-notes.md#期間の上限)）に収まる

Mackerel のアラートグループ設定（`alertGroupSettings`）でまとめる案もある。ただし設定がない組織が多く、範囲が重なりうるので、まずは監視単位にする（[未決](#未決) T3）。

## 集める情報

| 情報 | 出力のキー | API | 条件 |
|---|---|---|---|
| アラート | `alerts` | `GET /alerts`（開いているもの、`nextId` をたどる）、ID 指定なら `GET /alerts/<id>` | 常に |
| 監視の設定 | `monitor` | `GET /monitors`（1 回で全件） | 常に |
| ホスト | `alerts[].host` | `GET /hosts`（全状態、1 回）。一覧にないものは `GET /hosts/<id>`（退役済み） | アラートに `hostId` があるとき |
| アラートの経過 | `alerts[].logs` | `GET /alerts/<id>/logs` | ID を指定したときだけ（件数が増えるため） |
| 同じホストの他の監視 | `context.otherMonitors` | `GET /hosts/<id>/monitored-statuses` | ホストがあるとき。OK 以外だけ載せる |
| メトリクス | `context.metrics` | ホストの監視は `GET /hosts/<id>/metrics`、サービスの監視は `GET /services/<name>/metrics` | 監視の `metric` が取れる名前のとき（[未決](#未決) T2） |
| 最新のメトリクス時刻 | `alerts[].host.lastMetricAt` | `GET /tsdb/latest`（audit と同じ 3 つのメトリクス） | 疎通監視（`connectivity`）のアラート |
| アノテーション | `context.annotations` | `GET /graph-annotations` | 監視の `service`・`scopes` か、ホストのロールからサービスが分かるとき |
| 有効なダウンタイム | `context.downtimes` | `GET /downtimes`（1 回） | 監視・サービス・ロールがダウンタイムの範囲に入るとき |

- メトリクスは範囲内の点をそのまま返す（範囲が 30 分なら 1 分刻みで約 30 点）。しきい値と見比べられるよう、`monitor` の `warning`・`critical` はそのまま載せる

## 出力

```jsonc
{
  "generatedAt": 1794049200,
  "summary": {"open": 12, "critical": 3, "warning": 9, "unknown": 0, "groups": 4},
  "groups": [
    {
      "status": "CRITICAL",
      "openedAt": 1794048709,
      "monitor": {"id": "...", "name": "disk usage", "type": "host", "isMute": false, "...": "監視の設定そのまま"},
      "alerts": [
        {"id": "...", "status": "CRITICAL", "openedAt": 1794048709, "value": 96.2, "message": "...",
         "host": {"id": "...", "name": "web-3", "status": "working", "roles": ["web:app"], "lastMetricAt": null}}
      ],
      "context": {
        "range": {"from": 1794047809, "to": 1794049200},
        "metrics": [{"hostId": "...", "name": "...", "points": [{"time": 1794047820, "value": 91.0}]}],
        "otherMonitors": [{"hostId": "...", "monitorId": "...", "name": "cpu high", "status": "WARNING"}],
        "annotations": [{"title": "deploy a1b2c3d", "from": 1794048500, "to": 1794048620, "service": "web"}],
        "downtimes": []
      },
      "notes": [
        {"kind": "recent-annotation", "message": "an annotation \"deploy a1b2c3d\" started 3m before the alert"}
      ],
      "next": [
        "mcrl alert get <id> --expand",
        "mcrl metric get --host web-3 --name ... --since 3h"
      ],
      "errors": []
    }
  ],
  "otel": [
    {
      "serviceName": "checkout-api",
      "range": {"from": 1794047809, "to": 1794049200},
      "logs": [{"timestamp": "...", "severity": "error", "body": "...", "...": "API の形のまま", "truncated": false}],
      "traces": [{"traceId": "...", "title": "[HTTP]GET", "traceLatencyMillis": 2100, "...": "API の形のまま"}],
      "next": [
        "mcrl log search --service checkout-api --since 30m --severity error+",
        "mcrl trace list --service checkout-api --since 30m --status error --sort latency"
      ],
      "errors": []
    }
  ]
}
```

- 各値は API の形をなるべく崩さない（[decisions.md](decisions.md) D6 の方針）。ただしアラートのホストやグループは triage が組み立てた形になる。その形は `--help` に書き、1.0.0 までに固める
- `--max-groups` を超えたグループは `context` を持たず、`alerts` と `notes`（追加の取得が要らないものだけ）を載せる
- `next` は、このグループをさらに調べるためのコマンド。そのままコピーして実行できる形にする
- `errors` には、取れなかった情報を `{"source": "logs", "message": "..."}` の形で入れる。権限不足・レート制限・期間の上限などで、ほかの情報まで捨てないため

## ログ・トレース

ログ・トレースの `service.name`（OpenTelemetry のリソース属性）と、Mackerel のサービス・ロールは **まったく別の概念** で、API にも両者の対応はない（[api-notes.md](api-notes.md#ログトレースapm実測-2026-11-07)）。名前が同じでも同じものを指すとは限らない。ログもトレースも、検索には `serviceName`（`service.name`）が必須で、それがアラートの側からは決まらない。ホストで絞ろうにも、ログの検索条件にはリソース属性（`host.name` など）がない（レスポンスには `resourceAttributes` が付くので、サービスで検索した後に手元で絞ることはできる）。そのため、アラート（監視・ホスト・Mackerel のサービス）からログ・トレースを自動でたどることはしない。

- `--otel-service` で指定されたサービスについてだけ、グループとは独立に `otel` に並べる。範囲は、周辺情報を取るグループの中で最も早い開始時刻を `t` として `[t - window, min(t + window, 今)]`（グループと同じ考え方で、トレース検索の 30 分の上限に収まる）
- ログは `log search --severity error+` と同じ条件で新しい順に、トレースは `trace list --status error --sort latency` と同じ条件で、それぞれ `--max-samples` 件
- ログの本文は長いことがあるので、1 件あたりの `body` を一定の長さで切る（切ったら `"truncated": true`）。エージェントの文脈を食いつぶさないため
- 指定がなければ `otel` は空配列。対応表を設定ファイルに持たせる案（例: ロール `web:app` → `checkout-api`）は、使ってみて毎回同じ指定をするようなら検討する（[未決](#未決) T1）

### 注記（notes）

決まった規則で付ける。重要度は付けない（障害の最中に重要度を判定し直すのは人の仕事なので）。

| kind | 条件 | 判定の共有 |
|---|---|---|
| `monitor-muted` | 監視がミュート中（アラートはあるが通知されていない） | audit `muted-monitor` |
| `in-downtime` | 有効なダウンタイムの範囲に入っている | `downtime list --active` |
| `host-retired` | ホストが退役済み（片付け忘れのアラート） | audit `alert-on-retired-host` |
| `host-not-working` | ホストが `standby`・`maintenance`・`poweroff` | |
| `metrics-stopped` | 疎通監視のアラートで、最後のメトリクスがアラートの開始より前（エージェントの停止・ホストの停止を区別する手がかり） | audit `host-stale-metrics` |
| `recent-annotation` | 範囲内にアノテーションがある（デプロイなどの変更） | |
| `many-hosts` | 同じ監視で 2 台以上が鳴っている（共通の原因の可能性） | |
| `other-monitors-failing` | 同じホストで別の監視も OK 以外 | |

## リクエスト数と所要時間

一覧の取得は 1 回ずつで使い回す（audit と同じ）。グループごとの取得は、ホストあたり最大 2 回（メトリクス・他の監視の状態）と、Mackerel のサービスあたり 1 回（アノテーション）。ログ・トレースは `--otel-service` で指定したサービスあたり 2 回。

- 既定（5 グループ、各 3 ホスト、`--otel-service` なし）では、最大でおよそ `一覧 5 + 5 ×（3 × 2 + 1）= 40` リクエスト。並行して取るので（同時に 4 本まで）数秒で終わる
- ログとトレースは API の上限が 1 リクエスト/秒なので、`--otel-service` 1 つにつき 2 秒ほど増える。`internal/httpx` のレート制御はそのまま使う

## MCP

- 参照だけのコマンドなので、`mcrl mcp` の既定（参照系）で公開する（[decisions.md](decisions.md) D20）
- 説明文（`Long`）は、エージェントが「障害対応の最初に呼ぶ」と分かるように書く。`next` のコマンドは、同じ MCP サーバーのツールで続けて調べられる
- 出力が大きくなりすぎないよう、上限のフラグ（`--max-*`）の既定は控えめにする。MCP の prompts（「このアラートを調べて」）は必要になってから検討する

## 実装の方針

- `internal/cli/triage.go` に置く。アラートの展開（`expandAlerts`、`monitorIndex`）、audit の判定（ミュート・退役・メトリクスの途絶）、`log search`・`trace list` のリクエストの組み立ては、今のコードを関数として共有する
- テストは偽 API の txtar で、グループのまとめ方・上限・一部の取得の失敗（`errors` に入って終了コード 0）を確かめる
- 実 API では、検証用組織の擬似ホスト（[mcrl-fake-host-1](../../dev/fake-host/)）を止めて疎通監視のアラートを起こし、`metrics-stopped` が付くことを確かめる

## 作らないもの

- 原因の推定、要約の文章、スコアリング。事実と規則による注記だけを返す
- アラートを閉じる・メモを書く・ミュートするなどの操作（既存のコマンドで行う）
- 追従（`--watch`）。状況が変わったらもう一度叩く。継続して見たいなら `log tail` を使う

## 未決

| # | 論点 | 案 |
|---|---|---|
| T1 | 毎回同じ `--otel-service` を指定するなら、アラートの対象（ロールなど）から OpenTelemetry のサービスへの対応表を設定ファイルに持たせるか | まずは明示的な指定だけ。自動の対応づけはしない（両者は別の概念で、API に対応がないため） |
| T2 | ホストのメトリクス監視の `metric` は `cpu%` のような別名を取りうる。別名から `GET /hosts/<id>/metrics` の名前への対応 | 実 API で別名の一覧と対応を確かめる。対応が分からないものはメトリクスを載せず `errors` に理由を書く |
| T3 | まとめる単位。監視ごとか、Mackerel のアラートグループ設定に従うか | まずは監視ごと。アラートグループ設定を使う組織が増えたら `--group-by alert-group` を検討 |
| T4 | ダウンタイム中に監視の対象になったとき、アラートは作られるのか、通知だけ止まるのか | ドキュメントと実 API で確かめる。作られないなら `in-downtime` の注記は不要 |
| T5 | 一部の取得に失敗したときの終了コード | 0 にして `errors` に書く（上の案）。CI で使う用途はないので、失敗を終了コードで知らせる必要は薄い |
