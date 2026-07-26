# Hooks

[`run`](commands.md#run) can execute a command of your choice around the update: `--pre-exec` before it, `--post-exec` after it. Use them to guard an update behind a check, snapshot state, regenerate lockfiles, or notify once versions moved.

```console
clover run --pre-exec 'make snapshot' --post-exec 'make lock'
```

A failing pre-exec hook (any non-zero exit) **aborts the run** before any lookup or write. The post-exec hook runs once the update finished and produced a summary - even when some markers failed, since successful updates may already be on disk and still need the follow-up. It is skipped when the pre-exec hook aborted or the run errored before producing a summary. A failing post-exec hook makes `clover run` exit non-zero, and it never masks marker failures: both facts are reported.

A [`--dry-run`](commands.md#run) previews without side effects, so it skips both hooks entirely.

## Environment

Before the update a hook cannot know whether anything will change; after it, Clover does know. The distinction travels in environment variables, always all set, so a stale inherited value can never leak a false fact into a hook:

| Variable         | Pre       | Post                                                            |
| ---------------- | --------- | --------------------------------------------------------------- |
| `CLOVER_PHASE`   | `pre`     | `post`                                                          |
| `CLOVER_CHANGED` | `unknown` | `true` when the run rewrote anything, else `false`              |
| `CLOVER_SUCCESS` | `unknown` | `false` when any marker failed or a write errored, else `true`  |

A post-exec hook that should act only on change branches on `CLOVER_CHANGED`:

```console
clover run --post-exec '[ "$CLOVER_CHANGED" = true ] && make lock || true'
```

## Shell

Hook commands run through `/bin/sh -c` (on Windows, `cmd.exe /C`), inheriting Clover's working directory, environment, and standard streams. `--exec-shell` (or the `CLOVER_HOOK_SHELL` environment variable) swaps in another shell, invoked as `<shell> -c <command>` - the convention `bash`, `zsh`, `fish`, and `pwsh` all accept (`cmd` keeps its `/C`):

```console
clover run --exec-shell fish --post-exec 'test "$CLOVER_CHANGED" = true; and make lock'
```
