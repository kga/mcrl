# Configuration and authentication

[Documentation](README.md)

## Logging in

```sh
mcrl auth login          # paste an API key; it is verified and stored in the OS keychain
mcrl auth status         # which profile, where the key comes from, which organization
mcrl auth logout         # remove the stored key
```

`auth login` checks the key with `GET /api/v0/org` before storing it. Where no OS keychain is available (a headless Linux server, for example), it asks whether to store the key in plaintext in the config file instead; `--insecure-storage` does that without asking. To read the key from stdin rather than a prompt, use `--with-token`:

```sh
op read op://Private/Mackerel/credential | mcrl auth login --with-token
```

There is no flag that takes the API key itself, because command lines end up in shell history and `ps`.

You do not have to log in at all: mcrl can also take the key from an environment variable or from a command such as a password manager. See [Where the API key comes from](#where-the-api-key-comes-from).

## Profiles

A profile is the connection to one Mackerel organization. Use several to switch between organizations (production and staging, work and personal) or to reach Mackerel for KCPS.

```toml
# ~/.config/mcrl/config.toml
default_profile = "work"

[profiles.work]
# the key was stored with `mcrl auth login --profile work`

[profiles.staging]
api_key_command = "op read op://Infra/mackerel-staging/credential"

[profiles.kcps]
base_url = "https://kcps-mackerel.io"
```

| Key | Meaning |
|---|---|
| `default_profile` | Profile used when none is chosen |
| `base_url` | API base URL. Default `https://api.mackerelio.com` |
| `api_key_command` | Command whose output is the API key ([secret managers](secret-managers.md)) |
| `api_key` | Plaintext API key, written by `auth login --insecure-storage`. Last resort |

`mcrl auth login --profile <name>` creates the profile. The first profile you log in to becomes the default; `mcrl auth switch <name>` changes it, and `mcrl auth list` lists the profiles.

### Which profile is used

1. `--profile <name>`
2. the `MCRL_PROFILE` environment variable
3. `default_profile` in the config file
4. `default`

`mcrl auth status` shows the profile and where it came from (`profile.source`).

### Pinning a profile per directory

mcrl does not look for configuration files in the current directory. To use a different organization per repository, set `MCRL_PROFILE` with a tool that sets environment variables per directory:

```toml
# mise.toml (https://mise.jdx.dev/)
[env]
MCRL_PROFILE = "staging"
```

```sh
# .envrc (https://direnv.net/)
export MCRL_PROFILE=staging
```

## Where the API key comes from

mcrl uses the first key it finds:

| Order | Source | Typical use |
|---|---|---|
| 1 | `MACKEREL_APIKEY` environment variable | CI, containers, `op run` and similar wrappers, a one-off override |
| 2 | The profile's `api_key_command` (its standard output) | Password managers and secret stores |
| 3 | The OS keychain (service `mcrl`, account = profile name) | Your own machine; written by `auth login` |
| 4 | The profile's `api_key` (plaintext) | Machines without a keychain |

- `MACKEREL_APIKEY` is the only environment variable mcrl reads for the key, the same one other Mackerel tools use.
- The environment variable wins even over `--profile`. When you pass `--profile` and the key still comes from `MACKEREL_APIKEY`, mcrl prints a warning, so that you do not use another organization's key by accident.
- `mcrl auth status` and `--debug` show which source was used. `--debug` never prints the key.
- When a command fails with "not found" or an authentication error, the error's hint says which source the key came from, and the profile and base URL used (for example `using the API key from env:MACKEREL_APIKEY (profile "work", https://api.mackerelio.com)`). If a resource is "not found" unexpectedly, check that `MACKEREL_APIKEY` (which wins over the profile) is not set for another organization.
- `mcrl auth token` prints the key of the profile, for passing it to other tools.

### API key permissions

Mackerel API keys are Read or Write. Each command's `--help` ends with the endpoints it calls and the permission it needs:

```
API: POST /api/v0/hosts/<id>/retire, POST /api/v0/hosts/bulk-retire
API key permission: Read, Write
```

A 403 from a command that writes means the key is read-only, or the request comes from outside the organization's IP restriction.

## Environment variables

| Variable | Meaning |
|---|---|
| `MACKEREL_APIKEY` | API key (overrides the key stored for the profile) |
| `MCRL_API_BASE` | API base URL (overrides the profile's `base_url`) |
| `MCRL_PROFILE` | Profile to use |
| `MCRL_CONFIG` | Path of the config file |
| `MCRL_CACHE_DIR` | Cache directory |
| `MCRL_OUTPUT` | Default output format: `json` or `jsonl` |
| `MCRL_COLOR` | `auto`, `always` or `never` |
| `NO_COLOR` | Disable colors when set |
| `MCRL_NO_INPUT` | Never prompt when set |
| `MCRL_DEBUG` | Log HTTP requests and responses to stderr |
| `MCRL_EXTENSIONS_DIR` | Directory of installed [extensions](extensions.md) |
| `VISUAL`, `EDITOR` | Editor for the `edit` commands |
| `HTTPS_PROXY`, `NO_PROXY` | Proxy settings |

`mcrl help environment` prints the same list.

## Files

| What | Path | Override |
|---|---|---|
| Config file | `$XDG_CONFIG_HOME/mcrl/config.toml` (`~/.config/mcrl/config.toml`; `%APPDATA%\mcrl\config.toml` on Windows) | `MCRL_CONFIG` |
| Cache | `$XDG_CACHE_HOME/mcrl/` (`~/.cache/mcrl/`) | `MCRL_CACHE_DIR` |

macOS uses `~/.config` as well, not `~/Library/Application Support`. `mcrl config path` prints the paths in use.

mcrl writes the config file with mode 0600. The cache holds candidates for shell completion for 5 minutes; `mcrl config clear-cache` deletes it. Commands themselves never use the cache: names are always looked up in the API.

## Checking the setup

`mcrl doctor` checks the config file, where the key comes from, the connection, whether the key is valid, clock skew and proxy settings, and prints what it found. Run it before asking for help.
