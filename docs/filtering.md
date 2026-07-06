# Filtering

`include` and `exclude` narrow the set of candidate versions before selection, and `behind` steps back from the newest.

## include / exclude

Use `include` to keep only matching candidates, or `exclude` to drop them. Each takes a glob or a `/regex/`, and you can list either key more than once to apply several patterns.

```dockerfile
# clover: provider=docker repository=redis include=*-alpine constraint=minor
FROM redis:7.2.0-alpine
```

This keeps Clover on the `-alpine` variant instead of letting it wander onto a plain tag.

```dockerfile
# clover: provider=docker repository=redis exclude=*-rc* constraint=minor
FROM redis:7.2.0
```

List a key more than once to accept several patterns. Here either an `-alpine` or a `-slim` tag qualifies:

```dockerfile
# clover: provider=docker repository=redis include=*-alpine include=*-slim constraint=minor
FROM redis:7.2.0-alpine
```

## behind

`behind=N` selects the Nth version behind the newest, after all other filtering. Use it to stay one or more releases back from the bleeding edge.

```dockerfile
# clover: provider=github repository=redis/redis behind=1 constraint=minor
FROM redis:7.2.0
```

For an age-based delay rather than a fixed offset, see [Cooldown](cooldown.md).

## tag-prefix

A monorepo publishes component-scoped tags such as `storage/v1.40.0` and `pubsub/v1.33.0`, where the prefix names the component and the rest is its version. `tag-prefix` scopes selection to one component. It keeps only tags under the prefix, orders them by the version after it, and strips the prefix on render so the line pins the bare version.

```yaml
# clover: provider=github repository=owner/monorepo tag-prefix=storage/ constraint=minor
version: v1.40.0
```

With tags `storage/v1.40.0`, `storage/v1.41.0`, and `pubsub/v1.33.0`, this resolves `v1.41.0` (the newest `storage/`), never the unrelated `pubsub/` line. The prefix governs selection only, and the value written stays `v1.41.0`.
