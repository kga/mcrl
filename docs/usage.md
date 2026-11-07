# Using mcrl

[Documentation](README.md)

## Commands

```
mcrl <resource> <verb> [arguments] [flags]
```

Resources are singular (`host`, `monitor`); plurals work as aliases. The common verbs are `list` (`ls`), `get` (`show`), `create` (`new`), `update`, `edit` and `delete` (`rm`), plus verbs of their own such as `host retire` and `alert close`. Flags can go anywhere on the line.

Every command's `--help` has examples, the API endpoints it calls and the API key permission it needs. The [command reference](reference/README.md) collects all of them.

## Naming resources

Most commands take names as well as IDs:

```sh
mcrl monitor get 'cpu high'
mcrl host get web-1
mcrl dashboard edit overview
```

- An argument that looks like an ID (11 letters and digits) is tried as an ID first. Prefix it with `id:` or `name:` to force either.
- Hosts also take `cid:<custom identifier>`. Host names match exactly, including case.
- A name that matches several resources is an error that lists the candidates; mcrl never picks one for you.
- Roles are written `<service>:<role>`.
- Retired hosts are not in the host list, so give them by `id:`.

## Reading arguments from stdin

`-` in place of the arguments reads them from stdin, one per line. Together with `--jq` this chains commands:

```sh
mcrl host list --status poweroff --jq '.[].id' | mcrl host retire --yes -
mcrl alert list --monitor 'disk usage' --jq '.[].id' | mcrl alert close - --reason "cleaned up"
```

## Times and durations

Flags that take a time (`--since`, `--until`, `--start`, ...) accept:

| Example | Meaning |
|---|---|
| `now` | now |
| `1h`, `7d` | that long ago |
| `-30m` | relative to now |
| `2026-11-06T10:00:00+09:00` | RFC 3339 |
| `2026-11-06 10:00` | local time |
| `@1762390800` | epoch seconds |

Durations (`--duration`) are written like `90m`, `2h` or `1d`.

## Output

Output is always JSON: indented and colored on a terminal, one line when piped.

- Lists are arrays and single resources are objects. The API's wrapper objects (`{"hosts": [...]}`) are removed; otherwise the API's fields, names and epoch-second times are printed as they are, so the [Mackerel API documents](https://mackerel.io/api-docs/) describe the output.
- `-o jsonl` prints one item per line, streaming where possible. Commands that never end, such as `log tail`, always print JSON Lines.
- `--jq <expr>` filters the output with a built-in jq, so jq need not be installed. String results are printed without quotes, so IDs can go straight to the next command. With `-o jsonl`, the expression is applied to each item.
- `--expand` (on some commands, such as `alert list`) adds related names under `_expanded`, apart from the API's own fields.
- Commands that update a resource print the resource after the update. When the API answers only `{"success": true}`, mcrl fetches the resource again; `--no-refetch` skips that.
- Commands that take several targets (`alert close`, `host retire`, ...) always print an array, even for one target.
- `--compact` prints one line on a terminal too. `--color`, `MCRL_COLOR` and `NO_COLOR` control colors.
- `mcrl api` prints the API's response unchanged.

```sh
mcrl alert list --expand --jq '.[] | [.status, ._expanded.monitor.name, ._expanded.host.name] | @tsv'
```

Only results go to stdout. Progress, warnings, prompts and errors go to stderr, so `2>/dev/null` always leaves JSON.

## Creating and updating

Frequent fields have flags; everything else comes from a file:

```sh
mcrl downtime create --name "emergency maintenance" --duration 2h --service web
mcrl monitor create -f monitor.yaml            # JSON or YAML, "-" for stdin
mcrl monitor template host > monitor.yaml      # a commented template to start from
echo 'warning: 85' | mcrl monitor update 'cpu high' -f -
```

- YAML keys are the API's JSON field names. Flags override values from the file.
- Updates merge your changes into the current resource, so fields you leave out are kept. This matters for APIs that replace the whole object.
- `edit` opens the resource as YAML in `$VISUAL` or `$EDITOR`, shows the diff after you save, and sends it after you confirm.
- `--dry-run` prints the request that would be sent and stops.

## Triaging alerts

`mcrl triage` gathers, in one read-only call, what you look at first when alerts fire. It changes nothing and reports facts, not a diagnosis; it is meant for the start of an incident, from a terminal or as an MCP tool.

```sh
mcrl triage                                   # every open alert
mcrl triage exampleAlr1                       # one alert, with its status history
mcrl triage --service web --status critical
mcrl triage --otel-service checkout-api       # also error logs and traces of an OpenTelemetry service
mcrl triage --jq '.groups[] | [.status, .monitor.name, (.alerts | length)] | @tsv'
```

- Alerts are grouped by monitor. Groups are ordered by their heaviest status (`CRITICAL`, `WARNING`, `UNKNOWN`), then by their newest alert.
- The first `--max-groups` groups (default 5) get a `context`, read from `--window` (default `15m`) before the group's earliest alert to `--window` after it (or now): the monitored metrics, other monitors that are not OK on the same hosts, graph annotations, and downtimes in effect. Hosts are read for up to `--max-hosts` per group (default 3), newest alerts first.
- Rate monitors (`cpu%`, `memory%`, `swap%`, `disk%`) have no metric of their own, so their source metrics are shown (`cpu.idle.percentage`, `memory.used` and `memory.total`, and so on); the alert's `value` has the rate.
- `notes` flag what rules can tell: a muted monitor, a downtime in effect now (downtimes stop new alerts, so it most likely started after the alert opened), a retired or stopped host, a connectivity alert whose host stopped sending metrics before it, an annotation (such as a deploy) in the range, several hosts on one monitor, other monitors failing on the same host.
- `next` lists mcrl commands to look further, ready to run.
- Logs and traces are read only for the OpenTelemetry services given with `--otel-service`, because their service names have nothing to do with Mackerel services and roles. Traces cover at most 30 minutes, so keep `--window` at `15m` or less to get them.
- `--skip` and `--only` choose among `metrics`, `monitors`, `annotations`, `downtimes`, `logs` and `traces`.
- When some of the context cannot be read (no permission, a rate limit), the rest is still returned with the failure in the group's `errors`, and the exit code is 0.

`mcrl triage --help` describes every field of the output.

## Confirmation and scripts

Commands that delete or cannot be undone (`delete`, `host retire`, `user remove`, ...) ask for confirmation. mcrl asks only when stdin and stderr are both terminals; anywhere else such a command fails with exit code 2 unless you pass `--yes`. `--no-input` (or `MCRL_NO_INPUT`) turns every prompt off, for CI and agents.

## Errors and exit codes

On a terminal, errors read like this:

```
error: monitor "cpu high" not found
  hint: run `mcrl monitor list` to see available monitors
```

When stderr is not a terminal, the same error is one line of JSON. `status` (the HTTP status) and `hint` appear when there is one:

```json
{"error":{"code":"not_found","message":"monitor \"cpu high\" not found"}}
```

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | other error (including 5xx responses after retries) |
| 2 | usage error, or confirmation needed without `--yes` |
| 3 | authentication or authorization error (no API key, 401, 403) |
| 4 | not found |
| 5 | invalid input or conflict, or a name matching several resources |
| 6 | rate limited after retries |
| 7 | network error |
| 8 | `mcrl audit --fail-on` found something at or above the given severity |
| 130 | interrupted |

`mcrl help exit-codes` prints the table. `mcrl check run` and `annotation wrap` run a command of yours and exit with its code.

## Shell completion

```sh
mcrl completion zsh > "${fpath[1]}/_mcrl"
mcrl completion bash > /etc/bash_completion.d/mcrl
mcrl completion fish > ~/.config/fish/completions/mcrl.fish
```

Host, monitor and other names are completed from the API and cached for 5 minutes. The release archives include these scripts in `completions/`.

## Debugging

`--debug` (or `MCRL_DEBUG`) logs each HTTP request and response to stderr. API keys, other credentials in headers, and secrets in request and response bodies (such as an AWS integration's `secretKey` or a Slack webhook URL) are replaced with `***`.
