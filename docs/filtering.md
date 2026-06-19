# Filtering

`include` and `exclude` narrow the set of candidate versions before selection; `behind` steps back from the newest.

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

List a key more than once to accept several patterns - here either an `-alpine` or a `-slim` tag qualifies:

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
