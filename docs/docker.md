# Docker

The Docker provider tracks image tags from a container registry.

```dockerfile
# clover: provider=docker repository=redis constraint=minor
FROM redis:7.2.0
```

## Keys

| Key                                   | Description                                                                                           |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `provider`                            | `docker`.                                                                                             |
| `repository`                          | The image repository (e.g. `redis`, `library/redis`, `team/app`).                                     |
| `registry`                            | The registry host. Defaults to Docker Hub; set it for other registries (e.g. `registry.example.com`). |
| [`constraint`](constraints.md)        | How far the tag may move (`major`/`minor`/`patch`, or a semver range).                                |
| [`include` / `exclude`](filtering.md) | Filter the candidate tags (e.g. select an `-alpine` variant).                                         |
| [`prerelease`](prereleases.md)        | Allow or exclude prerelease tags.                                                                     |
| [`cooldown`](cooldown.md)             | Require a minimum age before a tag is eligible.                                                       |
| [`track`](tracking.md)                | Track a floating tag (e.g. `latest`, `nonroot`) instead of selecting a version.                       |

```dockerfile
# clover: provider=docker repository=team/app registry=registry.example.com constraint=minor
FROM registry.example.com/team/app:1.4.0
```

## Tracking a floating tag

A tag like `latest` or `nonroot` does not change name, but the digest it points at drifts. Use [`track`](tracking.md) to keep the digest pin fresh while leaving the tag text alone, and [`value=sha256`](checksums.md) to render the digest.

```dockerfile
# clover: provider=docker track=*
FROM redis:latest@sha256:0000000000000000000000000000000000000000000000000000000000000000
```
