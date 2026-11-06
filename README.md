# mcrl

A command-line client for the [Mackerel](https://mackerel.io/) API.

Output is always JSON: indented and colored on a terminal, compact when piped.
Filter it with the built-in `--jq`.

> Status: early development (milestone M0). Only `mcrl api`, `auth`, `doctor`,
> `config path`, `version` and `completion` are available.

## Install

```sh
go install github.com/kga/mcrl/cmd/mcrl@latest
```

## Usage

```sh
mcrl auth login                       # verify an API key and store it in the OS keychain
mcrl api /org                         # any API endpoint; /api/v0 may be omitted
mcrl api /hosts --jq '.hosts[].name'  # filter with jq
mcrl api /alerts -F withClosed=true --paginate
mcrl doctor                           # diagnose configuration and connectivity
```

The API key can also be given with the `MACKEREL_APIKEY` environment variable.
See `mcrl help environment` and `mcrl help exit-codes`.

## Development

Design documents (in Japanese) are in [docs/](docs/README.md).

```sh
mise run test
mise run lint
mise run build
```

## License

MIT
