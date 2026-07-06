# Tracking floating refs

Some references are not versions at all. A Docker tag like `latest` or `nonroot`, or a GitHub Action pinned to a branch HEAD, keeps the same name while the digest or commit it points at drifts. `track` keeps that secure pin fresh without selecting a new version.

<!-- clover-lint-skip -->

```dockerfile
# clover: provider=docker track=*
FROM redis:latest@sha256:0000000000000000000000000000000000000000000000000000000000000000
```

<!-- clover-lint-skip -->

```yaml
# clover: provider=github track=main verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

On each run Clover re-resolves the digest (Docker) or commit (GitHub) and rewrites only the pin, leaving the `latest` / `main` text untouched.

## Naming the ref

| Value           | Meaning                                            |
| --------------- | -------------------------------------------------- |
| `track=*`       | Infer the floating ref already written on the line |
| `track=nonroot` | Track a named tag explicitly                       |
| `track=main`    | Track a named branch explicitly                    |

## Interaction with other keys

`track` replaces the selection stage, so it cannot be combined with the selection keys: [`constraint`](constraints.md), [`include` / `exclude`](filtering.md), [`behind`](filtering.md), [`prerelease`](prereleases.md), and `downgrade`.

Two things still apply:

- [`cooldown`](cooldown.md) holds back a digest or commit that is too fresh.
- [`verify` and `verify-branch`](verification.md) cross-check the resolved pin.
