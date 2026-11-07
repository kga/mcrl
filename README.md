> [!NOTE]
> mcrl is a proof of concept: a reimplementation of a command-line client for the Mackerel API, made to explore what such a tool could look like. It is not an official Mackerel product.

# mcrl

A command-line client for the [Mackerel](https://mackerel.io/) API.

- **JSON in, JSON out.** Output is always JSON (or JSON Lines): indented and colored on a terminal, compact when piped. A built-in `--jq` filters it, so no separate `jq` is needed.
- **Covers the whole API**, and `mcrl api` reaches anything else.
- **Names, not IDs.** Hosts, monitors, dashboards and the like can be given by name.
- **Safe by default.** Destructive commands ask for confirmation (or `--yes`), every update supports `--dry-run`, and updates merge into the current object so fields are never dropped by accident.
- **Built for scripts and agents.** Stable exit codes, errors as JSON on stderr when not on a terminal, `-` reads arguments from stdin.

> Status: pre-1.0. Command names, flags and output may still change.

## Install

Download an archive for your platform from [GitHub Releases](https://github.com/kga/mcrl/releases). Each archive contains the `mcrl` binary, shell completion scripts in `completions/`, and the licenses of mcrl and the code built into it (`LICENSE`, `THIRD_PARTY_LICENSES`).

Or use the container image (linux/amd64, linux/arm64):

```sh
docker run --rm -e MACKEREL_APIKEY ghcr.io/kga/mcrl:latest org get
```

Or build from source (Go 1.26 or later):

```sh
go install github.com/kga/mcrl/cmd/mcrl@latest
```

## Authenticate

```sh
mcrl auth login          # paste an API key; it is verified and stored in the OS keychain
mcrl auth status         # which profile, where the key comes from, which organization
```

mcrl can also take the key from `MACKEREL_APIKEY` or from a password manager, and use several organizations through profiles. See [Configuration and authentication](docs/configuration.md) and [Password managers and secret stores](docs/secret-managers.md).

## Examples

```sh
# Open alerts with their monitor and host names, as TSV
mcrl alert list --expand --jq '.[] | [.status, ._expanded.monitor.name, ._expanded.host.name] | @tsv'

# Close every alert of a monitor
mcrl alert list --monitor 'disk usage' --jq '.[].id' | mcrl alert close - --reason "cleaned up"

# Retire powered-off hosts (30 at a time behind the scenes)
mcrl host list --status poweroff --jq '.[].id' | mcrl host retire --yes -

# Silence a service for two hours
mcrl downtime create --name "emergency maintenance" --duration 2h --service web

# Change a monitor's threshold without touching the rest of it
echo 'warning: 85' | mcrl monitor update 'cpu high' -f -

# Edit a dashboard in $EDITOR, review the diff, then save
mcrl dashboard edit overview

# Record a deploy as a graph annotation
mcrl annotation wrap --service web --title "deploy $(git rev-parse --short HEAD)" -- ./deploy.sh

# Report a cron job's result as a check monitor (exit code 0/1/2 = OK/WARNING/CRITICAL)
mcrl check run --host "$(hostname)" --name backup -- /usr/local/bin/backup.sh

# Post metrics in the plugin format
./my-plugin.sh | mcrl metric post --host "$(hostname)" -

# Search and follow logs
mcrl log search --service web --since 1h --severity error+ --where 'res.status>=500'
mcrl log tail --service web --jq '[.timestamp, .severity, .body] | @tsv'

# Slow traces and HTTP routes
mcrl trace list --service web --min-latency 1s --sort latency
mcrl apm http --service web --sort p95

# Find forgotten settings: muted monitors, ended downtimes, hosts that stopped
# sending metrics, unused channels, users without 2FA, ... (read-only)
mcrl audit
mcrl audit --check muted-monitor,user-without-mfa --jq '.[] | [.kind, .resource.name] | @tsv'
mcrl audit --fail-on warning -o jsonl > audit.jsonl   # exit code 8 when a warning is found

# What is going on right now: open alerts grouped by monitor, with the hosts,
# other failing monitors, metrics, deploys and downtimes around them (read-only)
mcrl triage
mcrl triage --service web --otel-service checkout-api

# Anything else
mcrl api /org
```

## Commands

| Area | Commands |
|---|---|
| Hosts | `host list/get/create/update/edit/status/retire/role/monitor-status` |
| Services | `service list/create/delete`, `role list/create/delete` |
| Metrics | `metric get/latest/names/post`, `metadata list/get/set/delete` |
| Monitoring | `monitor list/get/create/update/edit/delete/mute/unmute/template`, `check list/get/report/run` |
| Alerts | `alert list/get/close/memo/logs`, `alert-group ...`, `downtime ...`, `triage` |
| Notifications | `channel ...`, `notification-group ...` (alias `ng`) |
| Dashboards | `dashboard ...`, `annotation list/create/update/delete/wrap` |
| Observability | `log search/tail/saved`, `trace list/get`, `apm http/db` |
| Organization | `org get`, `org ip-restriction get/set`, `user list/remove`, `invitation ...`, `aws ...` |
| Other | `audit`, `api`, `mcp`, `extension`, `auth`, `config`, `doctor`, `completion`, `version` |

Every command has examples in `mcrl <command> --help`, along with the API endpoint it calls and the API key permission it needs.

## Documentation

- [Using mcrl](docs/usage.md): naming resources, output and `--jq`, creating and updating, errors and exit codes, completion
- [Configuration and authentication](docs/configuration.md)
- [Password managers and secret stores](docs/secret-managers.md)
- [AI agents (MCP)](docs/ai-agents.md): `claude mcp add mcrl -- mcrl mcp`
- [Extensions](docs/extensions.md): add commands as `mcrl-<name>` executables
- [Command reference](docs/reference/README.md)

## Development

Design documents and the decision log (in Japanese) are in [docs/design/](docs/design/README.md).

```sh
mise run test
mise run lint
mise run build
```

## License

MIT
