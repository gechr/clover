# Commands

```text
clover <command>
```

| Command    | Alias | Description                                                        |
| ---------- | ----- | ------------------------------------------------------------------ |
| `init`     |       | Create a starter [`.clover.yaml`](configuration.md) interactively  |
| `run`      |       | Resolve version references and update them in place                |
| `lint`     |       | Check every directive resolves, offline and without writing        |
| `format`   | `fmt` | Canonicalize directive comments                                    |
| `annotate` |       | Add `provider=auto` directives to recognized version lines         |
| `login`    |       | Authenticate Clover with a provider (GitHub, GitLab, Gitea)        |
| `update`   | `up`  | Update Clover to the latest release via Homebrew                   |
| `version`  |       | Print version information                                          |

## `run`

Resolve every annotation and rewrite its target line.

```text
clover run [options] [<path>…]
```

| Option                  | Description                                                                             |
| ----------------------- | --------------------------------------------------------------------------------------- |
| `--infer`               | Also update lines [auto-detection](auto.md) recognizes, without requiring a directive   |
| `--enable <provider>`   | Resolve only these providers, skipping all others                                       |
| `--disable <provider>`  | Skip these providers, resolving all others                                              |
| `-t, --tag <tag>`       | Only process directives matching these tags                                             |
| `--to <version>`        | Pin matched markers to this exact version (implies `--downgrade --force`)               |
| `-n, --dry-run`         | Resolve and render but write nothing                                                    |
| `--[no-]cache`          | Reuse cached HTTP responses across runs (`--no-cache` fetches everything fresh)         |
| `--[no-]deep`           | Follow pagination to fetch every version (more accurate, but slower and more requests)  |
| `-y, --yes`             | Proceed without confirming a deep lookup                                                |
| `--[no-]downgrade`      | Allow selecting versions older than the current one                                     |
| `--cooldown <duration>` | Override every directive's [`cooldown`](cooldown.md), e.g. `72h` or `2w` (`0` disables) |
| `--[no-]prerelease`     | Allow selecting [prerelease](prereleases.md) versions                                   |
| `--[no-]force`          | Re-pin followed digests even when the version they follow is unchanged                  |
| `--[no-]verify`         | Verify secure pins against upstream tags (implies `--deep`)                             |
| `-o, --output <output>` | Output detail: `text` (default), `wide`, or `github`                                    |
| `--no-ignore`           | Scan files [`.gitignore`](ignore.md) would exclude (VCS directories stay excluded)      |
| `--config <path>`       | Path to a [`.clover.yaml`](configuration.md) file                                       |
| `--no-config`           | Do not load any `.clover.yaml` config                                                   |

With no paths, Clover scans the current directory. Pass files or directories to narrow the run. `--no-ignore` is also accepted by `lint` and `format`.

`--infer` is the zero-annotation mode: every line [auto-detection](auto.md) recognizes is updated as if it carried a bare `provider=auto` directive, without writing any comments. Written directives keep priority on their own lines (their `constraint` and other rules apply as usual), a [`clover:ignore`](ignore.md) control still opts a line out, and a recognized line that would not resolve is skipped rather than failing the run. Use [`annotate`](#annotate) instead when you want the directives in the file, where they can carry selection rules and document intent.

`--enable` and `--disable` narrow the run to a subset of providers, matched against the provider each marker resolves to (a `provider=auto` marker after inference, a follower after the producer it follows). `--enable` is authoritative, so `--enable=github,docker` resolves only GitHub and Docker markers and leaves every other line untouched. `--disable` is subtractive, so `--disable=node` resolves everything except Node markers. Both accept a comma-separated list or repeated flags, name any provider except `manual` (which owns its line and always runs), and cannot be combined. A skipped marker drops out of the run rather than reporting, exactly as an unmatched `--tag` would.

`--to` rewrites every matched marker to one explicit version instead of the newest allowed, which makes it the rollback tool. The version is picked from the upstream listing, so digests and checksums still resolve for a release that really exists, and a version the upstream does not publish fails the marker. The pin bypasses each directive's own selection rules ([`constraint`](constraints.md), [`include`/`exclude`](filtering.md), [`prerelease`](prereleases.md), [`cooldown`](cooldown.md), `behind`) and implies `--downgrade` and `--force`, though an explicit flag such as `--no-force` still wins. Every matched marker receives the same version, so combine it with paths, `-t`, or `--enable` to scope the pin. Markers that follow a floating ref (`track=`) or own their line (`manual`) resolve as usual.

## `lint`

Validate that every directive parses, resolves, and has a `find` pattern that matches the target line, without touching any files or making network calls. Useful in CI.

```text
clover lint [options] [<path>…]
```

`lint` accepts the same `-t/--tag` selection and `-o, --output` options as `run`.

## `format`

Rewrite directive comments into canonical form and key order, migrating deprecated spellings. No version changes.

```text
clover format [options] [<path>…]
```

| Option            | Description                                                                        |
| ----------------- | ---------------------------------------------------------------------------------- |
| `--check`         | Report directives that need formatting and exit non-zero without writing           |
| `-n, --dry-run`   | Report what would be reformatted without writing                                   |
| `--[no-]prune`    | Remove unknown keys instead of erroring on them                                    |
| `--no-ignore`     | Scan files [`.gitignore`](ignore.md) would exclude (VCS directories stay excluded) |
| `--config <path>` | Path to a [`.clover.yaml`](configuration.md) file                                  |
| `--no-config`     | Do not load any `.clover.yaml` config                                              |

`--check` is the CI gate, while `--dry-run` previews the same rewrites but exits zero.

## `annotate`

Add `clover: provider=auto` directives to lines Clover can already track but that carry none. For example, GitHub Actions `uses:` pins and container image references can be annotated automatically. It is the inverse of [auto-detection](auto.md). Rather than resolving an existing `provider=auto` marker, it finds the lines such a marker would resolve and writes one above each.

```text
clover annotate [options] [<path>…]
```

| Option            | Description                                                                        |
| ----------------- | ---------------------------------------------------------------------------------- |
| `--check`         | Report annotations that would be added and exit non-zero without writing           |
| `-n, --dry-run`   | Preview the proposed annotations without writing                                   |
| `-w, --write`     | Apply the proposed annotations                                                     |
| `--force`         | Rewrite an existing annotation into its canonical minimal form                     |
| `--no-ignore`     | Scan files [`.gitignore`](ignore.md) would exclude (VCS directories stay excluded) |
| `--config <path>` | Path to a [`.clover.yaml`](configuration.md) file                                  |
| `--no-config`     | Do not load any `.clover.yaml` config                                              |

Unlike `run` and `format`, `annotate` previews by default and writes only with `--write`, since it inserts new lines. Set [`annotate.write`](configuration.md) to opt in to writing by default, or [`annotate.check`](configuration.md) to make annotate a default CI gate. `--dry-run`, `--write`, and `--check` override the configured mode for one invocation. Every annotation is verified offline first, and unresolved lines are left alone.

Pass global `--verbose` with `annotate` to show recognized candidates Clover deliberately skipped, including the reason they failed validation or were opted out.

Existing annotations are never touched without `--force`. With it, an annotation Clover itself would produce (`provider=auto`, or an explicit provider the line infers) is collapsed back to `provider=auto`, dropping the `provider`/`repository`/`registry` keys that inference supplies (and `host`, when the line itself names one) while preserving every selection rule (`constraint`, `include`, `cooldown`, …). A deliberately explicit directive Clover cannot infer (`provider=http`, a `find`/`replace`, a tracked ref) is left untouched. A [`clover:ignore`](ignore.md) control opts a line out of annotation just as it opts it out of resolution.

## `login`

Authenticate with a provider (for higher rate limits or private sources). GitHub and GitLab use an OAuth device flow, while Gitea uses a browser-based loopback flow.

Pass `--host` to authenticate against a GitHub Enterprise Server, self-managed GitLab, or self-hosted Gitea instance. Such an instance runs its own OAuth application, so `--host` requires a matching `--client-id` (the public hosts use Clover's embedded app).

```bash
clover login                                              # GitHub (default)
clover login gitlab
clover login gitea --host git.example.com
clover login github --host ghe.example.com --client-id <id>
```

## `update`

Update Clover to the latest release through Homebrew, the sanctioned update path.

```text
clover update [options]
```

| Option     | Description                                              |
| ---------- | -------------------------------------------------------- |
| `--check`  | Report whether an update is available without installing |
| `--stable` | Install the latest stable release                        |
| `--dev`    | Install the latest source build                          |

Clover will periodically check for updates and hint when a newer release is available. Set `CLOVER_NO_UPDATE_CHECK=1` to disable it.
