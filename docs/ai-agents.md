# Using mcrl with AI agents (MCP)

[Documentation](README.md)

`mcrl mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io/) server on stdin and stdout, so AI agents such as Claude Code and Claude Desktop can call mcrl commands as tools.

- Each tool runs the mcrl command of the same name (`host_get` runs `mcrl host get`) and returns its JSON output. Tool arguments mirror the command's flags and arguments, plus `jq` for `--jq`.
- Names are resolved, input is checked and errors are classified exactly as on the command line. Errors come back as a tool error carrying the same JSON as stderr and the [exit code](usage.md#errors-and-exit-codes) in `exitCode`.

## What is exposed

| Server flags | Tools |
|---|---|
| none | Read-only commands |
| `--allow-write` | Also commands that create or update resources, with a `dry_run` argument |
| `--allow-destructive` | Also commands that ask for confirmation on the command line (`delete`, `host retire`, `user remove`, `invitation revoke`, `org ip-restriction set`, ...). They run without asking: your MCP client's approval of the tool call takes the place of the prompt |

Commands that open an editor (`edit`), run a local command (`check run`, `annotation wrap`), never end (`log tail`) or manage credentials (`auth`) are never exposed. `api` is exposed for GET requests only. A file given with `--file` becomes a `body` argument, since the server cannot read files on the agent's side.

Choose the narrowest set that does the job, and pair it with a read-only API key where you can.

## Setting it up

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

Global flags given to `mcrl mcp` (`--profile`, `--timeout`, `--debug`) apply to every tool call. With `--debug`, HTTP logs go to the server's stderr.

## The API key

The server finds the key [as mcrl always does](configuration.md#where-the-api-key-comes-from): `MACKEREL_APIKEY` (set it in the client's `env`), the profile's `api_key_command`, or the key stored by `mcrl auth login`.

`api_key_command` runs on every tool call. If that is slow or asks for approval each time, start the server with the key in its environment instead, so that it is fetched once:

```sh
claude mcp add mcrl -- op run --env-file=/path/to/mcrl.env -- mcrl mcp
```

See [secret managers](secret-managers.md).
