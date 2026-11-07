# Using a password manager or secret store

[Documentation](README.md) · [Configuration](configuration.md)

mcrl has no built-in support for any particular password manager. Instead it offers two generic ways in, and every tool that can print a secret or run a command with secrets in its environment fits one of them:

| Way | How | Good for |
|---|---|---|
| **Environment variable** | The tool runs mcrl with `MACKEREL_APIKEY` set | One-off commands, CI, the MCP server |
| **`api_key_command`** | mcrl runs a command and uses what it prints | Everyday use without wrapping every command |

The environment variable comes first in the [order mcrl looks for the key](configuration.md#where-the-api-key-comes-from), so a wrapper always wins over `api_key_command` and the keychain.

## `api_key_command`

```toml
# ~/.config/mcrl/config.toml
[profiles.work]
api_key_command = "op read op://Private/Mackerel/credential"
```

- The command runs **without a shell**. Arguments are split on spaces and quotes are honored, but `$VAR`, `~`, pipes and redirections are not. Wrap it in `sh -c '...'` when you need them.
- mcrl uses the command's standard output with surrounding whitespace removed. Empty output is an error.
- The command gets no standard input, so it cannot ask for a password on the terminal. Use the tool's desktop app integration, an agent, or a token in its environment.
- It times out after 30 seconds. On failure, mcrl shows the command's stderr.
- It runs **every time mcrl needs the key**: once per mcrl invocation, and once per tool call in `mcrl mcp`. For scripts that call mcrl many times, or for the MCP server, prefer the environment variable.

`mcrl auth status` shows `api_key_command` as the key's source when it works.

## Environment variable

```sh
<wrapper> -- mcrl host list
```

mcrl needs nothing configured; the wrapper (`op run`, `doppler run` and the like) sets `MACKEREL_APIKEY` for the command it runs. To give the MCP server a key this way, wrap the server itself, so that the key is fetched once when it starts:

```sh
claude mcp add mcrl -- op run --env-file=/path/to/mcrl.env -- mcrl mcp
```

## 1Password

With the [1Password CLI](https://developer.1password.com/docs/cli/) (`op`) and its desktop app integration:

```toml
# api_key_command: read a secret reference
[profiles.work]
api_key_command = "op read op://Private/Mackerel/credential"
```

```sh
# op run: put a secret reference in the environment, or in an env file
MACKEREL_APIKEY=op://Private/Mackerel/credential op run -- mcrl host list

echo 'MACKEREL_APIKEY=op://Private/Mackerel/credential' > mcrl.env
op run --env-file=mcrl.env -- mcrl host list

# op run with a 1Password Environment (1Password CLI 2.41 or later)
op run --environment <environment-id> -- mcrl host list
```

`op run` hides secrets that appear in the output. A secret reference contains no secret, so an env file of references can be committed.

## Other tools

The commands below follow each tool's documented way of printing one secret. Check your tool's documentation for the exact path or name, and try the command on its own first: it must print only the key.

| Tool | `api_key_command` |
|---|---|
| HashiCorp Vault | `vault kv get -field=apikey secret/mackerel` |
| AWS Systems Manager Parameter Store | `aws ssm get-parameter --name /mackerel/apikey --with-decryption --query Parameter.Value --output text` |
| Google Cloud Secret Manager | `gcloud secrets versions access latest --secret=mackerel-apikey` |
| pass | `pass show mackerel/work` |
| macOS Keychain (an item you created yourself) | `security find-generic-password -s mackerel -w` |

Tools that run a command with secrets in its environment (such as `doppler run --` or `infisical run --`) work with the [environment variable](#environment-variable) way, as long as the secret is named `MACKEREL_APIKEY`.

## Containers

The container image reads `MACKEREL_APIKEY`:

```sh
docker run --rm -e MACKEREL_APIKEY ghcr.io/kga/mcrl:latest org get
```

The image is distroless: it has no shell or other commands, so `api_key_command` cannot run anything in it. Set `MACKEREL_APIKEY` from your orchestrator's secret instead, for example with `env[].valueFrom.secretKeyRef` in Kubernetes.

When you run the mcrl binary in an image of your own that has a shell, a key mounted as a file (Docker or Kubernetes secrets) can also be read with `api_key_command = "cat /run/secrets/mackerel_apikey"` in a config file that `MCRL_CONFIG` points at.
