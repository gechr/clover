# Constraints

`constraint` bounds how far a version is allowed to move from the one currently on the line. Without it, Clover selects the newest eligible version.

## Keyword constraints

A keyword caps the bump relative to the current value:

| Constraint | Allows                                 |
| ---------- | -------------------------------------- |
| `major`    | any newer version                      |
| `minor`    | same major; newer minor or patch       |
| `patch`    | same major and minor; newer patch only |

```dockerfile
# clover: provider=docker repository=redis constraint=minor
FROM redis:7.2.0
```

With `redis` at `7.2.0`, `constraint=minor` will move to `7.4.0` but never to `8.x`.

## Range constraints

Instead of a keyword, give a semver range. Ranges combine comparators with commas:

```dockerfile
# clover: provider=docker repository=redis constraint=">=7.2,<8"
FROM redis:7.2.0
```

## Downgrades

Clover never selects an older version than the current one unless you allow it:

```bash
clover run --allow-downgrade
```

or per annotation with `allow-downgrade`. This is occasionally useful after an over-eager bump, or to pin back to a supported line.
