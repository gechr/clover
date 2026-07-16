# Cooldown

`cooldown` requires a version to be at least a certain age before Clover will adopt it. A fresh release is held back until it has had time to settle, which guards against yanked or quickly-patched versions.

```dockerfile
# clover: provider=github repository=redis/redis cooldown=72h constraint=minor
FROM redis:7.2.0
```

## Durations

The value is a duration. Hours work, and longer units compose:

| Value  | Meaning            |
| ------ | ------------------ |
| `72h`  | 72 hours           |
| `2w3d` | 2 weeks and 3 days |

A version that is newer than its cooldown is simply skipped, and Clover stays on the current value until the candidate ages in.

Cooldown needs a publication date to measure age against. Dated sources include HashiCorp and Node.js releases, classic Helm repositories, and forge releases (`source=releases`). Where the source dates nothing it returns - notably GitHub and Gitea tags (the default `source=tags`) - Clover cannot check the cooldown, so rather than silently update past it, the marker is **skipped with a warning** and the line is held. Switch to `source=releases`, or remove the cooldown, to update such a line. (GitLab tags may carry a date individually, so only the undated ones go unchecked.)

Cooldown still applies when [tracking floating refs](tracking.md). A digest or commit that is too fresh is held back even though no version is being selected.

## Precedence

Three places can set a cooldown, and they resolve in a fixed order. The `--cooldown` flag on [`run`](commands.md#run) overrides every directive for that invocation, and `--no-cooldown` disables cooldowns outright, which is useful when a fix must ship now. A directive's own `cooldown` key is next, a deliberate per-line choice. The [`run.cooldown`](configuration.md) config default comes last, filling in only for directives that carry no cooldown of their own.
