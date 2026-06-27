# Commands

```text
clover <command>
```

| Command   | Description                                                  |
| --------- | ------------------------------------------------------------ |
| `init`    | Create a starter `.clover.yaml` interactively.               |
| `run`     | Resolve version references and update them in place.         |
| `lint`    | Check every directive resolves, offline and without writing. |
| `format`  | Canonicalize directive comments.                             |
| `login`   | Authenticate Clover with a provider via its device flow.     |
| `update`  | Update Clover to the latest release via Homebrew.            |
| `version` | Print version information.                                   |

## `run`

Resolve every annotation and rewrite its target line.

```text
clover run [options] [<path>…]
```

| Option                   | Description                                                                             |
| ------------------------ | --------------------------------------------------------------------------------------- |
| `-t, --tag <tag>`        | Only process directives matching these tags.                                            |
| `-n, --dry-run`          | Resolve and render but write nothing.                                                   |
| `--deep`                 | Follow pagination to fetch every version - more accurate, but slower and more requests. |
| `-y, --yes`              | Proceed without confirming a deep lookup.                                               |
| `--[no-]downgrade`       | Allow selecting versions older than the current one.                                    |
| `--[no-]prerelease`      | Allow selecting [prerelease](prereleases.md) versions.                                  |
| `-o, --output <output>`  | Output detail: `text` (default) or `wide`.                                              |
| `--no-ignore`            | Scan files [`.gitignore`](ignoring.md) would exclude; VCS directories stay excluded.    |
| `--config <path>`        | Path to a `.clover.yaml` [config](configuration.md) file.                               |
| `--no-config`            | Do not load any `.clover.yaml` config.                                                  |

With no paths, Clover scans the current directory. Pass files or directories to narrow the run. `--no-ignore` is also accepted by `lint` and `format`.

## `lint`

Validate every directive - that it parses, resolves, and that its `find` pattern matches the target line - without touching any files or making network calls. Useful in CI.

```text
clover lint [options] [<path>…]
```

## `format`

Rewrite directive comments into canonical form and key order, migrating deprecated spellings. No version changes.

```text
clover format [options] [<path>…]
```

## `login`

Authenticate with a provider (for higher rate limits or private sources) via its device flow.

```bash
clover login
```

## `update`

Update Clover to the latest release through Homebrew, the sanctioned update path.

```text
clover update [options]
```

| Option     | Description                                                |
| ---------- | ---------------------------------------------------------- |
| `--check`  | Report whether an update is available without installing.  |
| `--stable` | Install the latest stable release.                         |
| `--dev`    | Install the latest source build.                           |

Clover will periodically check for updates and hint when a newer release is available. Set `CLOVER_NO_UPDATE_CHECK=1` to disable it.
