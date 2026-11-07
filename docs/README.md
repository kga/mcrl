# mcrl documentation

Guides for using mcrl. For installation and a quick start, see the [README](../README.md).

| Guide | Contents |
|---|---|
| [Using mcrl](usage.md) | Command grammar, naming resources, stdin, times, output and `--jq`, creating and updating, confirmation, errors and exit codes, completion |
| [Configuration and authentication](configuration.md) | Logging in, profiles, pinning a profile per directory, where the API key comes from, environment variables, files |
| [Password managers and secret stores](secret-managers.md) | `api_key_command` and environment variables with 1Password, Vault, cloud secret stores and others; containers |
| [AI agents (MCP)](ai-agents.md) | Running `mcrl mcp` for Claude Code and other MCP clients, what is exposed, the API key |
| [Extensions](extensions.md) | Installing, upgrading and writing `mcrl-<name>` extensions |
| [Command reference](reference/README.md) | Every command with its flags, examples, API endpoints and permission; which command calls each API endpoint. Generated from `--help` |

Everything here is also in `mcrl <command> --help`, `mcrl help environment` and `mcrl help exit-codes`.

## Design documents

The design documents and decision log (in Japanese) are in [design/](design/README.md). They describe why mcrl works the way it does, and include ideas that are not implemented yet; these guides describe what mcrl does today.
