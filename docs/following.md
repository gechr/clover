# Following values

A value resolved in one place can feed another. A **producer** publishes the version it resolves under an `id`, and a **follower** reads that value and renders it into its own line instead of contacting an upstream. This keeps related lines coherent without resolving the same thing twice.

```yaml
# clover: provider=github repository=redis/redis id=redis constraint=minor
appVersion: 7.2.0

# clover: from=redis
tag: 7.2.0
```

The first annotation resolves `redis` and publishes it as `id=redis`. The second omits `provider` and follows that id, so both lines always move together.

## Keys

| Key      | Description                                                                                            |
| -------- | ------------------------------------------------------------------------------------------------------ |
| `id`     | Publish this annotation's resolved value under a name                                                  |
| `from`   | Follow the value published under the given `id`                                                        |
| `select` | Which value to take from the source: `new` (the resolved value, default) or `old` (its previous value) |
| `value`  | What the follower projects, e.g. the [`version`, `commit`, or `sha256`](checksums.md)                  |

A follower may itself carry an `id`, so values chain across lines, files, and even repositories. Clover resolves producers before the followers that depend on them.

An `id` must be unique within its repository - two markers publishing the same one leave every `from=` ambiguous, so both `lint` and `run` reject a written duplicate rather than letting whichever resolved last win. An id [auto-detection](auto.md) inferred (a `<TOOL>_VERSION` variable paired with its checksum sibling) never contends with one you wrote: the inferred id yields, the line's version stays tracked, and the paired checksum follower is skipped with a reason - write `id=`/`from=` yourself to pin the pairing you meant.
