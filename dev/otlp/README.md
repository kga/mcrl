# OTLP テストデータ（開発用）

`mcrl log` / `mcrl trace` / `mcrl apm` を実データで試すために、検証用組織へテスト用のトレースとログを送る。

```sh
mise exec -- dev/otlp/send.sh
```

- サービス名 `mcrl-otel`、環境 `test` のトレース 3 本（`/users/{id}` 120ms、`/orders` 900ms でエラー、`/health` 3ms。それぞれ DB の子スパン付き）と、対応するログ 3 件を送る
- ログの本文は JSON（`{"req": {...}, "res": {"status": ...}, "msg": ...}`）なので `--where 'res.status>=500'` などを試せる
- 送り先（Mackerel のヘルプより）: トレース `https://otlp-vaxila.mackerelio.com/v1/traces`、ログ `https://otlp.mackerelio.com:4318/v1/logs`。ヘッダ `Mackerel-Api-Key` に Write 権限のキー
- 送ってから 1 秒ほどで検索できるようになる（2026-11-07 実測）。トレース検索・APM は直近 30 分までしか引けないので、送ってからすぐ試す
