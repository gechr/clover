# Manual

The manual provider is a value you maintain by hand. It resolves to whatever is already on the target line and publishes it under an `id` for [followers](following.md) to read - it contacts no upstream and never rewrites its own line.

```dockerfile
# clover: provider=manual id=nginx
ARG NGINX_VERSION=1.27.3
```

Use it to anchor a dependency graph at a value no registry can resolve: a version you bump deliberately, a private artifact with no queryable registry, or a reference you have chosen to pin by hand. The value moves only when a person edits the line; on each run Clover republishes it so [followers](following.md) and [side values](checksums.md) stay in step.

## Keys

| Key                      | Description                                                                                                            |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `provider`               | `manual`.                                                                                                              |
| `id`                     | Publish the line's value under this name for followers. Required - a manual root that publishes nothing has no effect. |
| [`find`](annotations.md) | Pin which token on the line is the value, when its shape is ambiguous.                                                 |

A manual marker takes none of the selection keys (`constraint`, `include`/`exclude`, `track`, `behind`, `prerelease`): there is no candidate set to choose from, only the value already written.

## Following a manual value

A follower reads the published `id` and renders it onto its own line, so several references move together when you bump the root by hand:

```dockerfile
# clover: provider=manual id=nginx
ARG NGINX_VERSION=1.27.3
```

```yaml
# clover: from=nginx
image: nginx:1.27.3
```

[Side values](checksums.md) work too: a follower can project a `sha256` for the manual version - sourced from a `sha256-url` templated with `{version}` - so a checksum tracks the hand-set version without a registry.

## What it does not do

You own the value, so a manual marker never edits its own line. It only reads and republishes it, leaving the line byte-for-byte unchanged - including a secure `@sha256:` pin - and reporting it as up to date.
