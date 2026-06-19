# Following values

A value resolved in one place can feed another. A **producer** publishes the version it resolves under an `id`; a **follower** reads that value and renders it into its own line instead of contacting an upstream. This keeps related lines coherent without resolving the same thing twice.

```yaml
# clover: provider=github repository=redis/redis id=redis constraint=minor
appVersion: 7.2.0

# clover: from=redis
tag: 7.2.0
```

The first annotation resolves `redis` and publishes it as `id=redis`. The second omits `provider` and follows that id, so both lines always move together.

## Keys

| Key      | Description                                                                                             |
| -------- | ------------------------------------------------------------------------------------------------------- |
| `id`     | Publish this annotation's resolved value under a name.                                                  |
| `from`   | Follow the value published under the given `id`.                                                        |
| `select` | Which value to take from the source: `new` (the resolved value, default) or `old` (its previous value). |
| `value`  | What the follower projects - e.g. the [`version`, `commit`, or `sha256`](checksums.md).                 |

A follower may itself carry an `id`, so values chain across lines, files, and even repositories. Clover resolves producers before the followers that depend on them.
