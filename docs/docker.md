# Docker

The Docker provider tracks image tags from a container registry.

```dockerfile
# clover: provider=docker repository=redis constraint=minor
FROM redis:7.2.0
```

## Keys

| Key                                   | Description                                                                                             |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `provider`                            | `docker`                                                                                                |
| `repository`                          | The image repository (e.g. `redis`, `library/redis`, `team/app`). May carry an inline host (see below). |
| `registry`                            | The registry host. Defaults to Docker Hub; set it for other registries (e.g. `registry.example.com`).   |
| `platform`                            | An `os/arch` (e.g. `linux/amd64`) to pin that platform's digest instead of the multi-arch index digest. |
| [`constraint`](constraints.md)        | How far the tag may move (`major`/`minor`/`patch`, or a semver range).                                  |
| [`include` / `exclude`](filtering.md) | Filter the candidate tags (e.g. select an `-alpine` variant).                                           |
| [`prerelease`](prereleases.md)        | Allow or exclude prerelease tags.                                                                       |
| [`cooldown`](cooldown.md)             | Require a minimum age before a tag is eligible.                                                         |
| [`track`](tracking.md)                | Track a floating tag (e.g. `latest`, `nonroot`) instead of selecting a version.                         |

```dockerfile
# clover: provider=docker repository=team/app registry=registry.example.com constraint=minor
FROM registry.example.com/team/app:1.4.0
```

## The registry host

The host can live in `registry` or inline at the front of `repository` - whichever reads more naturally. A `repository` whose first segment looks like a host (it contains a `.` or `:`, or is `localhost`) is split automatically, so a `docker pull`-shaped reference needs no separate `registry`:

```dockerfile
# clover: provider=docker repository=ghcr.io/owner/app constraint=minor
FROM ghcr.io/owner/app:1.4.0
```

An explicit `registry` always wins, so a repository path that genuinely starts with a dotted segment can still be addressed by setting `registry`.

## Pinning a platform's digest

By default a digest pin resolves the multi-arch *index* digest, so the pin works on every architecture that pulls it. Set `platform` to pin one platform's manifest digest instead - the digest `docker pull --platform <os/arch>` would resolve.

```dockerfile
# clover: provider=docker repository=redis platform=linux/amd64 constraint=minor
FROM redis:7.2.0@sha256:0000000000000000000000000000000000000000000000000000000000000000
```

Leave `platform` unset for the index digest; it is opt-in, never inferred from the host running Clover.

## Tracking a floating tag

A tag like `latest` or `nonroot` does not change name, but the digest it points at drifts. Use [`track`](tracking.md) to keep the digest pin fresh while leaving the tag text alone, and [`value=sha256`](checksums.md) to render the digest.

```dockerfile
# clover: provider=docker track=*
FROM redis:latest@sha256:0000000000000000000000000000000000000000000000000000000000000000
```
