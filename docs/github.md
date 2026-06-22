# GitHub

The GitHub provider tracks the releases and tags of a repository.

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

## Keys

| Key                                          | Description                                                                |
| -------------------------------------------- | -------------------------------------------------------------------------- |
| `provider`                                   | `github`.                                                                  |
| `repository`                                 | The `owner/name` of the repository to track.                               |
| `source`                                     | What to list: `tags` (default) or `releases` (required for `asset`).       |
| [`constraint`](constraints.md)               | How far the version may move (`major`/`minor`/`patch`, or a semver range). |
| [`include` / `exclude`](filtering.md)        | Filter the candidate tags.                                                 |
| `asset`                                      | Keep only releases publishing an asset whose filename matches (see below). |
| [`prerelease`](prereleases.md)               | Allow or exclude prerelease versions.                                      |
| [`cooldown`](cooldown.md)                    | Require a minimum age before a release is eligible.                        |
| [`track`](tracking.md)                       | Track a branch HEAD instead of selecting a version.                        |
| [`verify`, `verify-branch`](verification.md) | Deep-verify a secure pin against upstream.                                 |

## Selecting by asset

Some releases matter only when they ship a particular artifact - a Linux binary, say. `asset` keeps only releases whose asset list contains a filename matching its glob (or `/regex/`), then selects the newest of those. It requires `source=releases`, since only releases publish assets.

```yaml
# clover: provider=github repository=owner/tool source=releases asset=*linux_amd64.tar.gz constraint=minor
version: v1.4.0
```

Pair it with a [`value=sha256`](checksums.md) follower to source that asset's checksum alongside the version. `asset` selects which release; [`pattern`](checksums.md) selects which of its assets to hash.

## Authentication

Anonymous requests work but are rate-limited, and private repositories need a token. Authenticate once with the device flow:

```bash
clover login
```

## Pinning an action to a commit

GitHub Actions are commonly pinned to a commit SHA with the moving tag kept in a trailing comment. Clover keeps the pin fresh by tracking the branch and re-resolving its commit - see [Tracking](tracking.md) and [Verification](verification.md).

```yaml
# clover: provider=github track=main verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

To project the resolved commit into a target line, use [`value=commit`](checksums.md).
