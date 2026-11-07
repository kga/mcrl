# Extensions

[Documentation](README.md)

Extensions add commands to mcrl without changing it. `mcrl <name>` runs an executable named `mcrl-<name>` when `<name>` is not a built-in command. Extensions can be written in any language.

## Using extensions

```sh
mcrl extension install someone/mcrl-drill     # clone a GitHub repository named mcrl-<name>
mcrl extension install https://git.example.com/me/mcrl-drill.git
mcrl extension install ./mcrl-hello           # link a local directory while developing
mcrl extension list                           # source, URL and the installed commit
mcrl extension upgrade drill --dry-run        # see the new commit before upgrading
mcrl extension upgrade drill
mcrl extension remove drill
mcrl drill --help                             # run it
```

- `install` clones the default branch (shallowly) and prints the commit it installed. mcrl never updates an extension on its own; only `upgrade` moves it, to the latest commit of the default branch, discarding local changes in the clone.
- A local directory is linked, so your edits take effect at once.
- `git` is required for installing from a repository. Private repositories use your git credentials (for GitHub, `gh auth setup-git` sets them up).
- There is no download of release binaries. An extension built with Go or similar must work from its clone, or be installed into `PATH` some other way.

### Where mcrl looks

1. The extensions directory: `$MCRL_EXTENSIONS_DIR`, else `$XDG_DATA_HOME/mcrl/extensions` (`~/.local/share/mcrl/extensions`; `%LOCALAPPDATA%\mcrl\extensions` on Windows), as `mcrl-<name>/mcrl-<name>`
2. Each directory in `PATH`, as `mcrl-<name>`. Relative entries such as `.` are skipped

Extensions in `PATH` are not managed by mcrl: update and remove them the way you installed them.

Built-in commands, aliases, `help` and `completion` always win, so an extension cannot replace them. `mcrl --help` and completion list installed extensions separately as "Extension commands".

### Safety

An extension runs with your permissions, and through mcrl it can do anything your API key allows, including deleting resources with a Write key. Read its code before installing, check that the commit `install` prints is the one you read, and read the diff before `upgrade` (`--dry-run` shows the new commit first).

## Writing an extension

The smallest example is [examples/extensions/mcrl-hello](../examples/extensions/mcrl-hello/mcrl-hello):

```sh
#!/bin/sh
set -eu
exec "$MCRL_BIN" org get --jq '{greeting: "Hello, \(.name)!"}'
```

```sh
mcrl extension install ./examples/extensions/mcrl-hello
mcrl hello
```

- Name the repository or directory `mcrl-<name>` and put an executable `mcrl-<name>` directly in it. Names use lowercase letters, digits and hyphens. On Unix, make it executable. On Windows, mcrl looks for the extensions in `PATHEXT` (`.exe`, `.cmd`, ...), and runs an extension without one, such as a script with a shebang, with `sh` from `PATH` (Git for Windows).
- Arguments after the name, the standard streams and the exit code pass through unchanged. An extension that ends by a signal makes mcrl exit with 128 + the signal number.
- Implement `--help` yourself; mcrl does not generate help for extensions.

### Calling the API

Extensions get these environment variables:

| Variable | Value |
|---|---|
| `MCRL_BIN` | Absolute path of mcrl. Call it instead of whatever `mcrl` is in `PATH` |
| `MCRL_PROFILE` | The profile mcrl resolved |
| `MCRL_API_BASE` | The API base URL of that profile |

**The API key is never passed.** Call mcrl, which finds the key itself: `"$MCRL_BIN" host list`, or `"$MCRL_BIN" api /path` for raw API calls (with `--paginate`, or `-X POST -f file.json`). Retries, rate limiting, paging, error classification and `--debug` then work in your extension too. (If you have set `MACKEREL_APIKEY` yourself, extensions see it like any other environment variable.)

Since `MCRL_API_BASE` overrides the base URL, unset it when your extension calls mcrl with a different `--profile`.

### Global flags

Only global flags that can be passed on as environment variables may come before the extension's name: `--profile`, `-o`/`--output`, `--color`, `--debug` and `--no-input`. They reach the extension as `MCRL_PROFILE`, `MCRL_OUTPUT`, `MCRL_COLOR`, `MCRL_DEBUG` and `MCRL_NO_INPUT`, and mcrl called from the extension reads them too: inside `mcrl --profile prod drill`, `"$MCRL_BIN" api /org` talks to prod.

Other flags (`--jq`, `--dry-run`, `--yes`, ...) before the name are a usage error, since their meaning depends on the extension. Put them after the name for the extension to interpret.

### Conventions

- Print JSON, so that `--jq` and other commands can work with the output. Formatting mcrl's own output with `"$MCRL_BIN" ... --jq` is the easy way.
- Write errors to stderr, and use [mcrl's exit codes](usage.md#errors-and-exit-codes), or pass mcrl's own exit code through.
- Never change resources without asking. Offer `--dry-run` and a confirmation prompt, and respect `MCRL_NO_INPUT`.
