# mcrl

A command-line client for the [Mackerel](https://mackerel.io/) API.

- **JSON in, JSON out.** Output is always JSON (or JSON Lines): indented and colored on a terminal, compact when piped. A built-in `--jq` filters it, so no separate `jq` is needed.
- **Covers the whole API**, and `mcrl api` reaches anything else.
- **Names, not IDs.** Hosts, monitors, dashboards and the like can be given by name.
- **Safe by default.** Destructive commands ask for confirmation (or `--yes`), every update supports `--dry-run`, and updates merge into the current object so fields are never dropped by accident.
- **Built for scripts and agents.** Stable exit codes, errors as JSON on stderr when not on a terminal, `-` reads arguments from stdin.

> Status: pre-1.0. Command names, flags and output may still change.

## Install

Download an archive for your platform from [GitHub Releases](https://github.com/kga/mcrl/releases). Each archive contains the `mcrl` binary and shell completion scripts in `completions/`.

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

The key is looked up in this order: the `MACKEREL_APIKEY` environment variable, the profile's `api_key_command` (for example a password manager), the OS keychain, then a plaintext `api_key` in the config file. Several organizations can be used through profiles:

```toml
# ~/.config/mcrl/config.toml
default_profile = "work"

[profiles.staging]
api_key_command = "op read op://Infra/mackerel-staging/credential"

[profiles.kcps]
base_url = "https://kcps-mackerel.io"
```

```sh
mcrl --profile staging host list
```

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
| Alerts | `alert list/get/close/memo/logs`, `alert-group ...`, `downtime ...` |
| Notifications | `channel ...`, `notification-group ...` (alias `ng`) |
| Dashboards | `dashboard ...`, `annotation list/create/update/delete/wrap` |
| Observability | `log search/tail/saved`, `trace list/get`, `apm http/db` |
| Organization | `org get`, `org ip-restriction get/set`, `user list/remove`, `invitation ...`, `aws ...` |
| Other | `audit`, `api`, `mcp`, `auth`, `config`, `doctor`, `completion`, `version` |

Every command has examples in `mcrl <command> --help`, along with the API endpoint it calls and the API key permission it needs.

## Output and exit codes

- `-o json` (default) or `-o jsonl` (one item per line, streamed where possible)
- `--jq <expr>`: strings are printed without quotes, so IDs can be piped to the next command
- Lists are arrays; the API's wrapper objects are removed. `mcrl api` prints responses as is
- `--expand` adds related names under `_expanded`, apart from the API's own fields

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | other error |
| 2 | usage error, or confirmation needed without `--yes` |
| 3 | authentication or authorization error |
| 4 | not found |
| 5 | invalid input, conflict, or a name matching several resources |
| 6 | rate limited |
| 7 | network error |
| 8 | `mcrl audit --fail-on` found something at or above the given severity |
| 130 | interrupted |

See `mcrl help environment` for environment variables.

## Use with AI agents (MCP)

`mcrl mcp` runs an MCP server on stdio, so AI agents can call mcrl commands as tools. Each tool runs the command of the same name (`host_get` runs `mcrl host get`) and returns its JSON output; arguments mirror the command's flags plus `jq`.

Only read-only commands are exposed by default. `--allow-write` adds commands that create or update resources (with a `dry_run` argument). Commands that ask for confirmation in the CLI, such as `delete` and `host retire`, are exposed only with `--allow-destructive`; your MCP client's tool approval then takes the place of the prompt. See `mcrl mcp --help`.

Claude Code:

```sh
claude mcp add mcrl -- mcrl mcp
claude mcp add mcrl-prod -- mcrl --profile prod mcp    # a specific profile
claude mcp add mcrl-write -- mcrl mcp --allow-write    # also allow updates
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "mcrl": {
      "command": "/path/to/mcrl",
      "args": ["mcp"]
    }
  }
}
```

The API key is resolved as usual: the key stored by `mcrl auth login`, or `MACKEREL_APIKEY` in `env`.

## Shell completion

```sh
mcrl completion zsh > "${fpath[1]}/_mcrl"     # zsh
mcrl completion bash > /etc/bash_completion.d/mcrl   # bash
mcrl completion fish > ~/.config/fish/completions/mcrl.fish
```

Host, monitor and other names are completed from the API and cached for 5 minutes.

## Development

Design documents (in Japanese) are in [docs/](docs/README.md).

```sh
mise run test
mise run lint
mise run build
```

## License

MIT
